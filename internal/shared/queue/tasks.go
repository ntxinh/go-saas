package queue

// Task type identifiers shared by producers (API) and consumers (the
// Task-6 worker).
const (
	TaskEmailInvite       = "email:invite"
	TaskInviteExpirySweep = "org:invite-expiry"
)
