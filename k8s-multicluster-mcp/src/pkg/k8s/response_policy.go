package k8s

// Collection response defaults keep broad discovery calls useful without
// flooding the MCP client. Callers can opt into a larger bounded result or use
// selectors/query terms to retrieve a narrower set.
const (
	DefaultAPIResourceLimit  = 100
	MaxAPIResourceLimit      = 500
	DefaultResourceListLimit = 50
	MaxResourceListLimit     = 200
	DefaultEventLimit        = 50
	MaxEventLimit            = 200
)
