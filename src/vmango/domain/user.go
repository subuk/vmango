package domain

import (
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Name           string
	HashedPassword []byte
}

func (user *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword(user.HashedPassword, []byte(password))
	return err == nil
}
