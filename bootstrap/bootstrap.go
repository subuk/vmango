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
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	logger.Info().Str("filename", configFilename).Msg("using configuration file")
	cfg, err := config.Parse(configFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %s\n", err)
		os.Exit(1)
	}
	connectionPool := libvirt.NewConnectionPool(cfg.LibvirtUri, logger.With().Str("component", "libvirt-connection-pool").Logger())
	machineRepo := libvirt.NewVirtualMachineRepository(connectionPool, cfg.LibvirtConfigDrivePool, cfg.LibvirtConfigDriveSuffix, logger.With().Str("component", "vm-repository").Logger())
	volumeRepo := libvirt.NewVolumeRepository(connectionPool)
	volumePoolRepo := libvirt.NewVolumePoolRepository(connectionPool)
	hostInfoRepo := libvirt.NewHostInfoRepository(connectionPool)
	keyRepo, err := filesystem.NewKeyRepository(cfg.KeyFile, logger.With().Str("component", "key-repository").Logger())
	if err != nil {
		logger.Error().Err(err).Msg("cannot initialize key storage")
		os.Exit(1)
	}
	netRepo := libvirt.NewNetworkRepository(connectionPool, cfg.Bridges)
	compute := libcompute.New(machineRepo, volumeRepo, volumePoolRepo, hostInfoRepo, keyRepo, netRepo)

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
