package bootstrap

import (
	"fmt"
	"net/http"
	"os"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/libvirt"
	"subuk/vmango/web"

	"github.com/rs/zerolog"
)

func Web(configFilename string) {
	cfg, err := config.Parse(configFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %s\n", err)
		os.Exit(1)
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	connectionPool := libvirt.NewConnectionPool(cfg.LibvirtUri, logger.With().Str("component", "libvirt-connection-pool").Logger())
	machineRepo := libvirt.NewVirtualMachineRepository(connectionPool)
	volumeRepo := libvirt.NewVolumeRepository(connectionPool)
	hostInfoRepo := libvirt.NewHostInfoRepository(connectionPool)
	compute := libcompute.New(machineRepo, volumeRepo, hostInfoRepo)

	webenv := web.New(cfg, logger, compute)
	server := http.Server{
		Addr:    cfg.Web.Listen,
		Handler: webenv,
	}
	logger.Info().Str("addr", server.Addr).Msg("staring server")
	if err := server.ListenAndServe(); err != nil {
		logger.Error().Err(err).Msg("serve failed")
		os.Exit(1)
	}
}
