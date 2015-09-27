package cloudmeta

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"net"
	"net/http"
	"vmango"
)

func Index(ctx *Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Logger.WithFields(logrus.Fields{
		"method":      "index",
		"path":        req.URL.String(),
		"remote_addr": req.RemoteAddr,
	}).Info("metadata request")
	fmt.Fprintf(w, "2009-04-04\n")
	return nil
}

func VersionRoot(ctx *Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Logger.WithFields(logrus.Fields{
		"method":      "version_root",
		"path":        req.URL.String(),
		"remote_addr": req.RemoteAddr,
	}).Info("metadata request")
	fmt.Fprintf(w, "meta-data\nuser-data\n")
	return nil
}

func MetadataList(ctx *Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Logger.WithFields(logrus.Fields{
		"method":      "metadata_list",
		"path":        req.URL.String(),
		"remote_addr": req.RemoteAddr,
	}).Info("metadata request")
	ipaddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return fmt.Errorf("cannot parse remote ip address: %s", err)
	}
	meta, err := ctx.Resolver.GetMeta(ipaddr)
	if err != nil {
		return fmt.Errorf("cannot get domain metadata: %s", err)
	}
	for key, _ := range meta {
		fmt.Fprintf(w, "%s\n", key)
	}
	return nil
}

func MetadataDetail(ctx *Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Logger.WithFields(logrus.Fields{
		"method":      "metadata_detail",
		"path":        req.URL.String(),
		"remote_addr": req.RemoteAddr,
	}).Info("metadata request")
	ipaddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return fmt.Errorf("cannot parse remote ip address: %s", err)
	}
	meta, err := ctx.Resolver.GetMeta(ipaddr)
	if err != nil {
		return fmt.Errorf("cannot get domain metadata: %s", err)
	}
	key := mux.Vars(req)["key"]
	value, exists := meta[key]
	if !exists {
		return vmango.NotFound(fmt.Sprintf("metadata key %s doesn't exist", key))
	}
	fmt.Fprintf(w, "%s\n", value)
	return nil
}

func Userdata(ctx *Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Logger.WithFields(logrus.Fields{
		"method":      "userdata",
		"path":        req.URL.String(),
		"remote_addr": req.RemoteAddr,
	}).Info("metadata request")
	ipaddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return fmt.Errorf("cannot parse remote ip address: %s", err)
	}
	usermeta, err := ctx.Resolver.GetUser(ipaddr)
	if err != nil {
		return fmt.Errorf("cannot get domain metadata: %s", err)
	}
	fmt.Fprintf(w, usermeta)
	return nil
}
