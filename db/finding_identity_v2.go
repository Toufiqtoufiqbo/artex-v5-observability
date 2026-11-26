package db

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FindingIdentityVersionV2 is the canonical version used for NEW findings.
// v1 (literal title) stays intact for rows already persisted: changing the
// meaning of a fingerprint in place would silently rewrite identity and could
// collide with an unrelated older row. v2 and v1 never share a prefix.
const FindingIdentityVersionV2 = "v2"

var (
	findingRequestRE = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([^\s"'<>]+)`)
	// First path segment starts with a letter/_ so HTTP/1.1 and //host are not
	// mistaken for endpoints.
	findingPathRE      = regexp.MustCompile(`/(?:[A-Za-z_][A-Za-z0-9._~%${}\-\[\]]*)(?:/[A-Za-z0-9._~%${}\-\[\]]*)*/?`)
	findingParamRE     = regexp.MustCompile(`(?i)\bparam(?:eter)?s?\b\s*(?:=|:)?\s*([A-Za-z_][A-Za-z0-9_.\-\[\]]*)`)
	findingFieldRE     = regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_.\-]*)\s+(?:field|parameter)\b`)
	findingJSONPairRE  = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_\-]*)"\s*:\s*"([^"]*)"`)
	findingFormPairRE  = regexp.MustCompile(`(?i)(?:^|[?&\s])([A-Za-z_][A-Za-z0-9_\-]*)\s*=\s*([^\s&;,]+)`)
	findingInjectionRE = regexp.MustCompile(`(?i)(or\s+1\s*=\s*1|--|'|<script|sleep\s*\(|benchmark\s*\(|union\s+select|%27|%20or)`)
	findingOpaqueRE    = regexp.MustCompile(`(?i)^[0-9]+$|^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$|^[0-9a-f]{16,}$`)
)

// Words that follow "parameter"/"... field" in prose but are not parameter
// names ("the search query parameter allowing ..." must not yield "allowing").
var findingParamStopwords = map[string]bool{
	"allow": true, "allows": true, "allowing": true, "and": true, "as": true,
	"at": true, "body": true, "by": true, "called": true, "endpoint": true,
	"field": true, "for": true, "form": true, "found": true, "from": true,
	"in": true, "is": true, "json": true, "named": true, "of": true,
	"on": true, "parameter": true, "parameters": true, "query": true,
	"request": true, "search": true, "that": true, "the": true, "to": true,
	"url": true, "used": true, "using": true, "via": true, "was": true,
	"which": true, "with": true,
}

// FindingResource is the structural identity of a finding. The human title is
// deliberately NOT part of it: two reports of the same issue on the same
// endpoint/parameter use different wording, while the same wording can describe
// two unrelated endpoints.
type FindingResource struct {
	Method   string // canonical HTTP method, empty when not stated
	Endpoint string // lowercased path + sorted query key names, numeric/UUID ids collapsed
	Params   string // sorted parameter/field names, comma separated
	Fallback string // normalized title, used only when no endpoint/param was found
}

func findingAddExplicitParam(dst map[string]bool, raw string) {
	k := strings.ToLower(strings.Trim(raw, "\"'`.,;:()[]"))
	if k != "" {
		dst[k] = true
	}
}

// Prose mentions ("the search query parameter allowing ...") need a stopword
// filter; a real wire parameter named "query" or "search" must NOT be filtered.
func findingAddProseParam(dst map[string]bool, raw string) {
	k := strings.ToLower(strings.Trim(raw, "\"'`.,;:()[]"))
	if k == "" || findingParamStopwords[k] {
		return
	}
	dst[k] = true
}

// canonicalFindingEndpoint strips host/port/scheme/query values and fragment,
// collapses opaque ids, and sorts query parameter names. It returns "" for
// strings that are not an HTTP endpoint (e.g. "HTTP/1.1 200").
func canonicalFindingEndpoint(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "\"'<>.,;"))
	if raw == "" {
		return ""
	}
	if i := strings.IndexAny(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" || strings.HasPrefix(u.Path, "//") {
		raw = findingPathRE.FindString(raw)
		if raw == "" {
			return ""
		}
		u, err = url.Parse(raw)
		if err != nil || u.Path == "" {
			return ""
		}
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	for i, s := range segments {
		if findingOpaqueRE.MatchString(s) {
			segments[i] = "*"
		}
	}
	path = strings.ToLower(strings.Join(segments, "/"))
	keys := make([]string, 0, len(u.Query()))
	for k := range u.Query() {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		path += "?" + strings.Join(keys, ",")
	}
	return path
}

// ExtractFindingResource derives the structural identity from the evidence
// first, then the summary, then the title. Evidence is the strongest source
// because it contains the actual request line and payload.
func ExtractFindingResource(name, summary, evidence string) FindingResource {
	var r FindingResource
	texts := []string{evidence, summary, name}
	for _, text := range texts {
		if m := findingRequestRE.FindStringSubmatch(text); len(m) == 3 {
			r.Method = strings.ToUpper(m[1])
			r.Endpoint = canonicalFindingEndpoint(m[2])
			break
		}
	}
	if r.Endpoint == "" {
		for _, text := range texts {
			if ep := canonicalFindingEndpoint(findingPathRE.FindString(text)); ep != "" {
				r.Endpoint = ep
				break
			}
		}
	}

	params := make(map[string]bool)
	if i := strings.IndexByte(r.Endpoint, '?'); i >= 0 {
		for _, k := range strings.Split(r.Endpoint[i+1:], ",") {
			findingAddExplicitParam(params, k)
		}
	}
	for _, text := range []string{name, summary, evidence} {
		for _, m := range findingParamRE.FindAllStringSubmatch(text, -1) {
			findingAddProseParam(params, m[1])
		}
		for _, m := range findingFieldRE.FindAllStringSubmatch(text, -1) {
			findingAddProseParam(params, m[1])
		}
	}
	for _, re := range []*regexp.Regexp{findingJSONPairRE, findingFormPairRE} {
		for _, m := range re.FindAllStringSubmatch(evidence, -1) {
			if len(m) == 3 && findingInjectionRE.MatchString(m[2]) {
				findingAddExplicitParam(params, m[1])
			}
		}
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r.Params = strings.Join(keys, ",")
	if r.Endpoint == "" && r.Params == "" {
		r.Fallback = normalizeIdentityText(FindingNameOrDefault(name, summary))
	}
	return r
}

// FindingFingerprintV2 is the canonical identity for new findings.
//
// Dimensions: task scope, vulnclass, HTTP method, endpoint (path + query key
// names), parameter/field names, and the affected-asset set. The literal human
// title is excluded whenever a structural resource was found; it is only a
// last-resort fallback for reports with no request/response evidence at all.
// Severity, summary prose, evidence bytes, worker, status and timestamps stay
// mutable provenance, as in v1.
func FindingFingerprintV2(taskID int64, vulnClass, name, summary, evidence string, assetIDs []int64) string {
	scope := "legacy"
	if taskID > 0 {
		scope = strconv.FormatInt(taskID, 10)
	}
	r := ExtractFindingResource(name, summary, evidence)
	parts := []string{
		FindingIdentityVersionV2,
		scope,
		normalizeIdentityText(vulnClass),
		r.Method,
		r.Endpoint,
		r.Params,
		r.Fallback,
		normalizeIdentityAssets(assetIDs),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return FindingIdentityVersionV2 + ":" + hex.EncodeToString(sum[:])
}
