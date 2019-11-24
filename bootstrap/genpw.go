package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh/terminal"
)

func GenPassword() {
	fmt.Fprintf(os.Stderr, "Password: ")
	passwordBytes, _ := terminal.ReadPassword(0)
	password := strings.TrimSpace(string(passwordBytes))
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Printf(string(hashedPassword))
	fmt.Fprintf(os.Stderr, "\n")
}
