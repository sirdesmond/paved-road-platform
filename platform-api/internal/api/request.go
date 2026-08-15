package api

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	pub "github.com/sirdesmond/paved-road-platform/platform-api/pkg/api"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// tierMax is the platform's ceiling per tier. Requests above it need a
// conversation, not a bigger number.
//
// These same numbers live in the admission policy's ConfigMap. That one is
// authoritative — this copy exists to give a better message, earlier. See the
// ADR-0004 amendment.
var tierMax = map[string]struct {
	CPU  string
	Pods int32
}{
	"dev":     {CPU: "8", Pods: 20},
	"staging": {CPU: "16", Pods: 50},
	"prod":    {CPU: "64", Pods: 200},
}

// Validate is a function rather than a method because the request type lives in
// pkg/api, where clients can import it. Methods can't cross package boundaries,
// and that's a happy accident: clients get the wire format and cannot get the
// rules. The CLI physically can't reimplement this.
func Validate(r pub.CreateEnvironmentRequest) []pub.ValidationError {
	var errs []pub.ValidationError

	if !nameRE.MatchString(r.Team) {
		errs = append(errs, pub.ValidationError{
			Field: "team",
			Message: fmt.Sprintf("%q must be lowercase letters, numbers and hyphens "+
				"(it becomes a namespace label and must be DNS-safe)", r.Team),
		})
	}

	// Newlines would let a caller inject extra git trailers or PR body content,
	// since both are line-oriented formats. Reject rather than sanitise.
	if strings.TrimSpace(r.Requester) == "" {
		errs = append(errs, pub.ValidationError{
			Field:   "requester",
			Message: "required: who is asking, so the change is attributable. The API opens the PR under a bot identity, so without this nobody knows who wanted it",
		})
	} else if strings.ContainsAny(r.Requester, "\r\n") {
		errs = append(errs, pub.ValidationError{
			Field:   "requester",
			Message: "must not contain line breaks",
		})
	}

	if strings.TrimSpace(r.Contact) == "" {
		errs = append(errs, pub.ValidationError{
			Field:   "contact",
			Message: "required: a Slack channel or rota, so we know who to page. Environments without an owner get reaped",
		})
	}

	max, ok := tierMax[r.Tier]
	if !ok {
		errs = append(errs, pub.ValidationError{
			Field:   "tier",
			Message: fmt.Sprintf("%q is not a tier. Use dev, staging or prod", r.Tier),
		})
		return errs // no point checking quotas against an unknown tier
	}

	if r.CPU != "" {
		want, err := resource.ParseQuantity(r.CPU)
		if err != nil {
			errs = append(errs, pub.ValidationError{
				Field:   "cpu",
				Message: fmt.Sprintf("%q isn't a quantity. Try \"2\" or \"500m\"", r.CPU),
			})
		} else if limit := resource.MustParse(max.CPU); want.Cmp(limit) > 0 {
			errs = append(errs, pub.ValidationError{
				Field: "cpu",
				Message: fmt.Sprintf("%s exceeds the %s ceiling of %s. "+
					"Ask in #platform if you genuinely need more, we'd rather raise it deliberately "+
					"than have you split into two environments", r.CPU, r.Tier, max.CPU),
			})
		}
	}

	if r.Pods > 0 && r.Pods > max.Pods {
		errs = append(errs, pub.ValidationError{
			Field:   "pods",
			Message: fmt.Sprintf("%d exceeds the %s ceiling of %d pods", r.Pods, r.Tier, max.Pods),
		})
	}

	return errs
}
