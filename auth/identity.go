package auth

import "golang.org/x/crypto/bcrypt"

type LocalUser struct{ ID, Name, Email, PasswordHash string }
type IdentityPolicy struct {
	Users         []LocalUser
	OIDCIssuer    string
	OIDCClientID  string
	AllowedEmails []string
}

func (policy IdentityPolicy) Eligible(p Principal) bool {
	if p.Method == "local" {
		for _, u := range policy.Users {
			if u.ID == p.LocalID {
				return true
			}
		}
		return false
	}
	if p.Method != "oidc" || policy.OIDCClientID == "" || p.Issuer != policy.OIDCIssuer || p.Subject == "" {
		return false
	}
	if len(policy.AllowedEmails) == 0 {
		return true
	}
	if !p.EmailVerified {
		return false
	}
	for _, email := range policy.AllowedEmails {
		if p.Email == email {
			return true
		}
	}
	return false
}

func (policy IdentityPolicy) VerifyPassword(id, password string) *Principal {
	for _, user := range policy.Users {
		if user.ID == id {
			if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
				return nil
			}
			return &Principal{Method: "local", LocalID: user.ID, Email: user.Email, Name: user.Name}
		}
	}
	// Unknown usernames still take a bcrypt verification path.
	_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$igHQGROHntvl05AztpfMeONSBDUsEbZHxayc5DOPTKIFX50WrHURS"), []byte(password))
	return nil
}
