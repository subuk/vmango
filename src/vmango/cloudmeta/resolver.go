package cloudmeta

type LibvirtResolver struct {
}

func NewLibvirtResolver() *LibvirtResolver {
	return &LibvirtResolver{}
}

func (resolver *LibvirtResolver) GetMeta(ipaddr string) (map[string]string, error) {
	meta := map[string]string{
		"instance-id": "i-231239",
		"hostname":    "configured-with-cloud-init.tld",
	}
	return meta, nil
}

func (resolver *LibvirtResolver) GetUser(ipaddr string) (string, error) {
	data := `#cloud-config
hostname: configured-with-cloud-init.tld
users:
  - name: root
    ssh-authorized-keys:
      - ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQC5plI2j1Uyhu2IbsQg5NMe+gLpT7RYxR68J2iBhNXX8rliwz+eiOo1G9T8SnuNGUS5oTJ05MY3V4IENf0x2+ObuuySTqt7T1Eb0Ujekyj9NdVvgYGbyHMcN9bXCvN7/FFAAu54x2D0v5gEjFA67C1ekdlUZvMKXk5KVjV7OXHfSh6JzSFgUTE9Naj3y694fg1ZlndTiwboJ/IZBRzbofOmLkDukfBx+9rBnBpilNj2jFi89MdAXfbnvs4GbIT7O8P8E0qDE+OMBACNPyrLQKVVep64F3R1OBFbedLiB7LzcG+5Ztwd+JvsoFZkIBfYWzWjg0EjX0mFL+mo8HvMjhRchhrfww6WZdhZ3Fwwhr2BzuVGp83FTx4HccVsgLmEWkjitd16q+hsH39zXYUS6Cb7ZYB9aoIq6iRk1Rms0wDQvtZ1OS6hSs2O9bT6ce8pcOU43dbJpeWHRWDpEdrbCMxkozpS5FVzVLNd8mtgIS4byuxM6QUGnGPcBS32HFjEFI1p37EK5MCt5nPYXiA0qfikSVAn3+t9fmel1McZ4AkXXlqmygiPRvPvl6XhuMHW4inF+INRgrRBEY0YMD0Jb8+Nleph9biyBaGvkGB57pdB/6w9UCQPdGHFZDeQHJ6ekZcIucMo09/Lw5AbpADJW4wfdaU6kfTObPW9RSQlM5chNQ== kubus@kubus
ssh_pwauth: True
disable_root: False
chpasswd:
  list: |
    root:secret
  expire: False
`
	return data, nil
}
