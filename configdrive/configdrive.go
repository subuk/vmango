package configdrive

type Data interface {
	Hostname() string
	PublicKeys() []string
}
