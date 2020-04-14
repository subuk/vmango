package bootstrap

import (
	"fmt"
	"net/http"
	"os"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/configdrive"
	"subuk/vmango/filesystem"
	"subuk/vmango/libvirt"
	"subuk/vmango/util"
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
	volumeMetadata := map[string]libcompute.VolumeMetadata{}
	for _, image := range cfg.Images {
		volumeMetadata[image.Path] = libcompute.VolumeMetadata{
			OsName:    image.OsName,
			OsVersion: image.OsVersion,
			OsArch:    libcompute.NewArch(image.OsArch),
			Protected: image.Protected,
		}
	}

	keyRepo, err := filesystem.NewKeyRepository(util.ExpandHomeDir(cfg.KeyFile), logger.With().Str("component", "key-repository").Logger())
	if err != nil {
		logger.Error().Err(err).Msg("cannot initialize key storage")
		os.Exit(1)
	}

	epub := filesystem.NewScriptedComputeEventBroker(logger.With().Str("component", "compute-event-broker").Logger())
	for _, sub := range cfg.Subscribes {
		epub.Subscribe(sub.Event, sub.Script, sub.Mandatory)
	}

	nodeUri := map[string]string{}
	nodeOrder := []string{}
	settings := map[string]*libvirt.VirtualMachineRepositoryNodeSettings{}
	for _, c := range cfg.Libvirts {
		nodeUri[c.Name] = c.Uri
		nodeOrder = append(nodeOrder, c.Name)
		configDriveWriteFormat := configdrive.NewFormat(c.ConfigDriveWriteFormat)
		if configDriveWriteFormat == configdrive.FormatUnknown {
			logger.Error().
				Str("format", c.ConfigDriveWriteFormat).
				Strs("allowed", configdrive.AllFormatsStrings()).
				Msg("unknown libvirt configdrive write format")
			os.Exit(1)
		}
		settings[c.Name] = &libvirt.VirtualMachineRepositoryNodeSettings{
			ConfigDriveVolumePool:  c.ConfigDrivePool,
			ConfigDriveSuffix:      c.ConfigDriveSuffix,
			ConfigDriveWriteFormat: configDriveWriteFormat,
		}

	}
	connectionPool := libvirt.NewConnectionPool(nodeUri, nodeOrder, logger.With().Str("component", "libvirt-connection-pool").Logger())

	machineRepo := libvirt.NewVirtualMachineRepository(connectionPool, settings, logger.With().Str("component", "vm-repository").Logger())
	volumeRepo := libvirt.NewVolumeRepository(connectionPool, volumeMetadata)
	volpoolRepo := libvirt.NewVolumePoolRepository(connectionPool)
	nodeRepo := libvirt.NewNodeRepository(connectionPool)
	netRepo := libvirt.NewNetworkRepository(connectionPool)

	compute := libcompute.New(epub, machineRepo, volumeRepo, keyRepo)
	network := libcompute.NewNetworkService(netRepo)
	keys := libcompute.NewKeyService(keyRepo)
	volpools := libcompute.NewVolumePoolService(volpoolRepo)
	nodes := libcompute.NewNodeService(nodeRepo)
	volumes := libcompute.NewVolumeService(volumeRepo)

	webenv := web.New(cfg, logger, compute, network, keys, volpools, nodes, volumes)
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
