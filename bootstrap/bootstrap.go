package bootstrap

import (
	"fmt"
	"net/http"
	"os"
	"subuk/vmango/compute"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/configdrive"
	"subuk/vmango/filesystem"
	"subuk/vmango/libvirt"
	"subuk/vmango/util"
	"subuk/vmango/web"

	"github.com/rs/zerolog"
)

func migrateImageConfig(cfg *config.Config, imageStorage *filesystem.ImageManifestStorage, logger zerolog.Logger) {
	for _, imgConfig := range cfg.Images {
		_, err := imageStorage.Get(imgConfig.Path)
		if err == nil {
			logger.Warn().Str("path", imgConfig.Path).Msg("this image was migrated to image file, please remove it from configuration")
			continue
		}
		if err == compute.ErrImageManifestNotFound {
			if imgConfig.Hidden {
				logger.Warn().Str("path", imgConfig.Path).Msg("please move hidden volumes to libvirt.$id.hidden")
			}
			if imgConfig.OsName == "" {
				continue
			}
			osArch := compute.NewArch(imgConfig.OsArch)
			if osArch == compute.ArchUnknown {
				logger.Warn().Str("path", imgConfig.Path).Interface("valid_arches", compute.ValidArches).Msg("please set correct arch")
				continue
			}
			manifest := &compute.ImageManifest{
				VolumePath: imgConfig.Path,
				Os: compute.ImageManifestOs{
					Name:    imgConfig.OsName,
					Version: imgConfig.OsVersion,
					Arch:    osArch,
				},
			}
			if err := imageStorage.Save(manifest); err != nil {
				logger.Error().Str("path", imgConfig.Path).Err(err).Msg("cannot migrate image from config file to image file")
				os.Exit(1)
			}
			continue
		}
		logger.Error().Err(err).Msg("cannot migrate images config to image file")
		os.Exit(1)
	}
}

func Web(configFilename string) {
	zerolog.DurationFieldInteger = true
	fmt.Fprintf(os.Stderr, "Using configuration file '%s'\n", configFilename)

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	cfg, err := config.Parse(configFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %s\n", err)
		os.Exit(1)
	}
	switch cfg.LogLevel {
	default:
		fmt.Fprintf(os.Stderr, "Unknown log level %s, available levels: debug, info, warning, error\n", cfg.LogLevel)
		os.Exit(1)
	case "debug":
		logger = logger.Level(zerolog.DebugLevel)
	case "info":
		logger = logger.Level(zerolog.InfoLevel)
	case "warning":
		logger = logger.Level(zerolog.WarnLevel)
	case "error":
		logger = logger.Level(zerolog.ErrorLevel)
	}

	logger.Info().Str("filename", configFilename).Msg("using configuration file")

	keyRepo, err := filesystem.NewKeyRepository(util.ExpandHomeDir(cfg.KeyFile), logger.With().Str("component", "key-repository").Logger())
	if err != nil {
		logger.Error().Err(err).Msg("cannot initialize key storage")
		os.Exit(1)
	}
	imageStorage, err := filesystem.NewImageManifestStorage(util.ExpandHomeDir(cfg.ImageFile))
	if err != nil {
		logger.Error().Err(err).Msg("cannot initialize images storage")
		os.Exit(1)
	}

	migrateImageConfig(cfg, imageStorage, logger)

	epub := filesystem.NewScriptedComputeEventBroker(logger.With().Str("component", "compute-event-broker").Logger())
	for _, sub := range cfg.Subscribes {
		epub.Subscribe(sub.Event, sub.Script, sub.Mandatory)
		logger.Info().
			Str("event", sub.Event).
			Str("script", sub.Script).
			Bool("mandatory", sub.Mandatory).
			Msg("new script subscription created")
	}

	nodeUri := map[string]string{}
	nodeOrder := []string{}
	vmRepSettings := map[string]libvirt.NodeSettings{}
	vmManSettings := map[string]compute.VirtualMachineManagerNodeSettings{}
	hiddenVolumes := libvirt.VolumeRepositoryHiddenVolumes{}
	for _, c := range cfg.Libvirts {
		nodeUri[c.Name] = c.Uri
		nodeOrder = append(nodeOrder, c.Name)
		configDriveWriteFormat := configdrive.NewFormat(c.ConfigDriveWriteFormat)
		hiddenVolumes.Add(c.Name, c.HiddenVolumes...)
		if configDriveWriteFormat == configdrive.FormatUnknown {
			logger.Error().
				Str("format", c.ConfigDriveWriteFormat).
				Strs("allowed", configdrive.AllFormatsStrings()).
				Msg("unknown libvirt configdrive write format")
			os.Exit(1)
		}
		vmRepSettings[c.Name] = libvirt.NodeSettings{
			CdSuffix: c.ConfigDriveSuffix,
			Cache:    c.Cache,
		}
		vmManSettings[c.Name] = compute.VirtualMachineManagerNodeSettings{
			CdPool:   c.ConfigDrivePool,
			CdSuffix: c.ConfigDriveSuffix,
			CdFormat: configDriveWriteFormat,
		}

	}
	connectionPool := libvirt.NewConnectionPool(nodeUri, nodeOrder, logger.With().Str("component", "libvirt-connection-pool").Logger())

	vmRepo := libvirt.NewVirtualMachineRepository(connectionPool, vmRepSettings, logger.With().Str("component", "vm-repository").Logger())
	volumeRepo := libvirt.NewVolumeRepository(connectionPool, hiddenVolumes, imageStorage, logger.With().Str("component", "volume-repository").Logger())
	volpoolRepo := libvirt.NewVolumePoolRepository(connectionPool, logger.With().Str("component", "vol-pool-repository").Logger())
	nodeRepo := libvirt.NewNodeRepository(connectionPool, logger.With().Str("component", "node-repository").Logger())
	netRepo := libvirt.NewNetworkRepository(connectionPool, logger.With().Str("component", "net-repository").Logger())

	network := libcompute.NewNetworkService(netRepo)
	keys := libcompute.NewKeyService(keyRepo)
	volpools := libcompute.NewVolumePoolService(volpoolRepo)
	nodes := libcompute.NewNodeService(nodeRepo)
	volumes := libcompute.NewVolumeService(volumeRepo)
	vms := libcompute.NewVirtualMachineService(vmRepo)
	images := libcompute.NewImageManifestService(imageStorage)
	vmanager := libcompute.NewVirtualMachineManager(vms, volumes, epub, vmManSettings)

	webenv := web.New(cfg, logger, network, keys, volpools, nodes, volumes, vms, images, vmanager)
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
