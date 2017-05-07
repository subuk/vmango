package models

type SSHKey struct {
	Name   string
	Public string
}

type SSHKeyList []*SSHKey
