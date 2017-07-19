package web

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
)

type LogRequestMiddleware struct {
	logger          *logrus.Logger
	excludePrefixes []string
	trustedProxies  []string
}

func NewLogRequestMiddleware(trustedProxies, exclude []string) *LogRequestMiddleware {
	log := logrus.New()
	log.Level = logrus.InfoLevel
	log.Formatter = &logrus.TextFormatter{}

	return &LogRequestMiddleware{
		logger:          log,
		excludePrefixes: exclude,
		trustedProxies:  trustedProxies,
	}
}

func (mw *LogRequestMiddleware) getRemoteAddr(req *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	trusted := false
	for _, trustedAddr := range mw.trustedProxies {
		if trustedAddr == remoteHost {
			trusted = true
			break
		}
	}
	if !trusted {
		return remoteHost
	}
	if realIP := req.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	if xForwardedFor := req.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
		splitted := strings.SplitN(xForwardedFor, ",", 2)
		return strings.TrimSpace(splitted[0])
	}
	return remoteHost
}

func (mw *LogRequestMiddleware) ServeHTTP(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	for _, p := range mw.excludePrefixes {
		if strings.HasPrefix(req.URL.Path, p) {
			next(rw, req)
			return
		}
	}
	start := time.Now()
	entry := logrus.NewEntry(mw.logger)
	entry = entry.WithField("path", req.URL.Path)
	entry = entry.WithField("remote", mw.getRemoteAddr(req))
	next(rw, req)
	entry = entry.WithField("latency", time.Since(start))
	entry.Info("completed handling request")
}
