package bootstrap

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func GenPassword() {
	if err := genPassword(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot generate password: %s\n", err)
		os.Exit(1)
	}
}

func genPassword(input *os.File, output, prompt io.Writer) error {
	fmt.Fprint(prompt, "Password: ")
	passwordBytes, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(prompt)
	if err != nil {
		return fmt.Errorf("read from an interactive terminal: %w", err)
	}
	password := strings.TrimSpace(string(passwordBytes))
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(output, string(hashedPassword))
	return err
}
