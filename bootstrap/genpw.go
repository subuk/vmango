package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func GenPassword() {
	fmt.Fprintf(os.Stderr, "Password: ")
	passwordBytes, _ := term.ReadPassword(0)
	password := strings.TrimSpace(string(passwordBytes))
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(hashedPassword))
	fmt.Fprintf(os.Stderr, "\n")
}
