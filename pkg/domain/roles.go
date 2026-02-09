package domain

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) IsValid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleMember
}

func (r Role) String() string {
	return string(r)
}
