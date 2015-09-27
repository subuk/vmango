package cloudmeta

import (
	"bytes"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"net"
	"net/http"
	"os/exec"
)

type Server struct {
	router *mux.Router
}

func New() *Server {
	router := mux.NewRouter().StrictSlash(true)

	ctx := &Context{
		Logger:   logrus.New(),
		Resolver: NewLibvirtResolver(),
	}

	router.Handle("/", NewHandler(ctx, Index))
	router.Handle("/2009-04-04/", NewHandler(ctx, VersionRoot))
	router.Handle("/2009-04-04/meta-data/", NewHandler(ctx, MetadataList))
	router.Handle("/2009-04-04/meta-data/{key:[^/]+}", NewHandler(ctx, MetadataDetail))
	router.Handle("/2009-04-04/user-data", NewHandler(ctx, Userdata))

	return &Server{
		router: router,
	}
}

func (server *Server) SetupIPTables(addr string) error {
	var out bytes.Buffer

	addcmd := exec.Command(
		"iptables", "-t", "nat", "-I", "PREROUTING",
		"-d", "169.254.169.254", "-p", "tcp", "--dport", "80",
		"-j", "DNAT", "--to-destination", addr)
	addcmd.Stderr = &out
	if err := addcmd.Run(); err != nil {
		return fmt.Errorf("cannot add iptables rule for redirect from 169.254.169.254 to %s: %s", addr, out.String())
	}
	out.Reset()

	addcmd2 := exec.Command(
		"iptables", "-t", "nat", "-I", "OUTPUT",
		"-d", "169.254.169.254", "-p", "tcp", "--dport", "80",
		"-j", "DNAT", "--to-destination", addr)
	addcmd2.Stderr = &out
	if err := addcmd2.Run(); err != nil {
		return fmt.Errorf(out.String())
	}
	return nil
}

func (server *Server) CleanupIPTables(addr string) error {
	var out bytes.Buffer

	delcmd := exec.Command(
		"iptables", "-t", "nat", "-D", "PREROUTING",
		"-d", "169.254.169.254", "-p", "tcp", "--dport", "80",
		"-j", "DNAT", "--to-destination", addr)
	delcmd.Stderr = &out
	if err := delcmd.Run(); err != nil {
		return fmt.Errorf(out.String())
	}
	out.Reset()
	delcmd2 := exec.Command(
		"iptables", "-t", "nat", "-D", "OUTPUT",
		"-d", "169.254.169.254", "-p", "tcp", "--dport", "80",
		"-j", "DNAT", "--to-destination", addr)
	delcmd2.Stderr = &out
	if err := delcmd2.Run(); err != nil {
		return fmt.Errorf(out.String())
	}
	return nil
}

func (server *Server) ListenAndServe(addr string) error {

	socket, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind metadata server: %s", err)
	}

	server.CleanupIPTables(addr)
	if err := server.SetupIPTables(addr); err != nil {
		return fmt.Errorf("cannot add iptables rule for redirect from 169.254.169.254 to %s: %s", addr, err)
	}

	return http.Serve(socket, server.router)
}
