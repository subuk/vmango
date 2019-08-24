package util

import (
	"fmt"
	os_user "os/user"
	"strings"
)

func ExpandHomeDir(filename string) string {
	if strings.HasPrefix(filename, "~/") {
		user, err := os_user.Current()
		if err != nil {
			panic(fmt.Errorf("cannot get current user: %s", err))
		}
		return user.HomeDir + filename[1:]
	}
	return filename
}
