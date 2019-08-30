package bootstrap

import (
	"fmt"
	"net/http"
	"os"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/filesystem"
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
	machineRepo := libvirt.NewVirtualMachineRepository(connectionPool, cfg.LibvirtConfigDrivePool, cfg.LibvirtConfigDriveSuffix, logger.With().Str("component", "vm-repository").Logger())
	volumeRepo := libvirt.NewVolumeRepository(connectionPool)
	hostInfoRepo := libvirt.NewHostInfoRepository(connectionPool)
	keyRepo := filesystem.NewKeyRepository(cfg.KeyFile, logger.With().Str("component", "key-repository").Logger())
	netRepo := libvirt.NewNetworkRepository(connectionPool, cfg.Bridges)
	compute := libcompute.New(machineRepo, volumeRepo, hostInfoRepo, keyRepo, netRepo)

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
