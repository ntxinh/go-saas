package events

import "github.com/google/uuid"

// Topics, dot-namespaced per event.
const (
	TopicMemberInvited  = "org.member_invited"
	TopicMemberJoined   = "org.member_joined"
	TopicMemberRemoved  = "org.member_removed"
	TopicRoleChanged    = "org.role_changed"
	TopicUserRegistered = "user.registered"
)

// MemberInvited fires when an org invite is created; the only wired
// subscriber enqueues TaskEmailInvite.
type MemberInvited struct {
	OrgID       uuid.UUID `json:"org_id"`
	OrgName     string    `json:"org_name"`
	Email       string    `json:"email"`
	Token       string    `json:"token"`
	InviterName string    `json:"inviter_name"`
}

// MemberJoined fires when an invite is accepted. Published for the
// contract; no v1 subscriber.
type MemberJoined struct {
	OrgID  uuid.UUID `json:"org_id"`
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
}

// MemberRemoved fires when a member leaves/is removed.
type MemberRemoved struct {
	OrgID  uuid.UUID `json:"org_id"`
	UserID uuid.UUID `json:"user_id"`
}

// RoleChanged fires when a member's role changes.
type RoleChanged struct {
	OrgID  uuid.UUID `json:"org_id"`
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
}

// UserRegistered fires on first sign-up.
type UserRegistered struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
}
