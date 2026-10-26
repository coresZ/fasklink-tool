package model

// UserInfo is the traffic block returned by the panel / subscription header.
type UserInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}

// Account is one saved panel account. PasswordEnc is never serialized to the
// browser; the API returns HasPassword instead.
//
// Kind distinguishes the two supported shapes:
//   - "password": logs in with email + password through the Apex v2 API
//   - "link":     only holds a subscription URL, no credentials
type Account struct {
	Kind            string    `json:"kind"`
	ID              string    `json:"id"`
	Label           string    `json:"label"`
	Email           string    `json:"email"`
	PasswordEnc     string    `json:"-"`
	HasPassword     bool      `json:"hasPassword"`
	SubscriptionURL string    `json:"subscriptionUrl"`
	LastLogin       int64     `json:"lastLogin"`
	LastFetch       int64     `json:"lastFetch"`
	UserInfo        *UserInfo `json:"userInfo"`
	Error           string    `json:"error"`
}

// Account kinds.
const (
	KindPassword = "password"
	KindLink     = "link"
)

// Source is a resolved subscription source.
type Source struct {
	Kind  string // local, account, manual
	ID    string
	Label string
	URL   string
}

// Snapshot is one fetched and templated YAML profile.
type Snapshot struct {
	Source    string    `json:"source"`
	URL       string    `json:"url"`
	Label     string    `json:"label"`
	FetchedAt int64     `json:"fetchedAt"`
	YAML      string    `json:"yaml"`
	RawYAML   string    `json:"rawYaml"`
	UserInfo  *UserInfo `json:"userInfo"`
}

// Address is one reachable subscription URL shown by the UI. Each host exposes
// two variants: URL (Clash template applied) and RawURL (the provider's original
// config, before the template).
type Address struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	RawURL string `json:"rawUrl"`
}

// Node is a display-only entry parsed from the YAML.
type Node struct {
	Name   string `json:"name"`
	Server string `json:"server"`
}
