package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"subuk/vmango/api"
	"subuk/vmango/auth"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/configdrive"
	"subuk/vmango/filesystem"
	"subuk/vmango/libvirt"
	"subuk/vmango/tasks"
	"subuk/vmango/util"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

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

	definitions, volumeMetadata, err := cfg.ImageDefinitions()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid image configuration")
	}
	imageRepo, err := catalog.NewImageRepository(definitions)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize image catalog")
	}
	images := compute.NewImageService(imageRepo)

	keyRepo, err := filesystem.NewKeyRepository(util.ExpandHomeDir(cfg.KeyFile), logger.With().Str("component", "key-repository").Logger())
	if err != nil {
		logger.Error().Err(err).Msg("cannot initialize key storage")
		os.Exit(1)
	}

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
	nodeScopes := map[string]string{}

	nodeOrder := []string{}
	vmRepSettings := map[string]libvirt.NodeSettings{}
	vmManSettings := map[string]compute.VirtualMachineManagerNodeSettings{}
	for _, c := range cfg.Libvirts {
		nodeUri[c.Name] = c.Uri
		scopeHash := sha256.Sum256([]byte(c.Uri))
		nodeScopes[c.Name] = "libvirt:" + hex.EncodeToString(scopeHash[:])
		nodeOrder = append(nodeOrder, c.Name)
		configDriveWriteFormat := configdrive.NewFormat(c.ConfigDriveWriteFormat)
		if configDriveWriteFormat == configdrive.FormatUnknown {
			logger.Error().
				Str("format", c.ConfigDriveWriteFormat).
				Strs("allowed", configdrive.AllFormatsStrings()).
				Msg("unknown libvirt configdrive write format")
			os.Exit(1)
		}
		vmRepSettings[c.Name] = libvirt.NodeSettings{
			CdSuffix:             c.ConfigDriveSuffix,
			Emulator:             c.Emulator,
			QcowPreallocMetadata: c.QcowPreallocMetadata,
			Cache:                c.Cache,
		}
		vmManSettings[c.Name] = compute.VirtualMachineManagerNodeSettings{
			CdPool:   c.ConfigDrivePool,
			CdSuffix: c.ConfigDriveSuffix,
			CdFormat: configDriveWriteFormat,
		}

	}
	connectionPool := libvirt.NewConnectionPool(nodeUri, nodeOrder, logger.With().Str("component", "libvirt-connection-pool").Logger())

	vmRepo := libvirt.NewVirtualMachineRepository(connectionPool, vmRepSettings, logger.With().Str("component", "vm-repository").Logger())
	volumeRepo := libvirt.NewVolumeRepository(connectionPool, vmRepSettings, volumeMetadata, logger.With().Str("component", "volume-repository").Logger())
	volpoolRepo := libvirt.NewVolumePoolRepository(connectionPool, logger.With().Str("component", "vol-pool-repository").Logger())
	nodeRepo := libvirt.NewNodeRepository(connectionPool, logger.With().Str("component", "node-repository").Logger())
	netRepo := libvirt.NewNetworkRepository(connectionPool, logger.With().Str("component", "net-repository").Logger())

	network := libcompute.NewNetworkService(netRepo)
	keys := libcompute.NewKeyService(keyRepo)
	volpools := libcompute.NewVolumePoolService(volpoolRepo)
	nodes := libcompute.NewNodeService(nodeRepo)
	volumes := libcompute.NewVolumeService(volumeRepo)
	vms := libcompute.NewVirtualMachineService(vmRepo)

	vmanager := libcompute.NewVirtualMachineManager(vms, volumes, epub, vmManSettings, images)
	cloudDefinitions, err := cfg.CloudDefinitions()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid cloud catalog")
	}
	clouds := &compute.CloudService{Definitions: cloudDefinitions, Nodes: nodes, VMs: vms, Pools: volpools, Volumes: volumes, Manager: vmanager}
	vmanager.Clouds = clouds
	vms.CloudMode = true
	ownershipRepo, err := filesystem.NewVolumeOwnershipRepository(util.ExpandHomeDir(cfg.Tasks.Directory) + "-volumes")
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize volume ownership")
	}
	defer ownershipRepo.Close()
	volumes.Ownership = ownershipRepo

	artifactRepo, err := filesystem.NewImageArtifactRepository(util.ExpandHomeDir(cfg.Tasks.Directory) + "-images")
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize image verification metadata")
	}
	defer artifactRepo.Close()
	imageBindings := map[string]compute.ImageBinding{}
	for _, c := range cfg.Libvirts {
		imageBindings[c.Name] = compute.ImageBinding{Scope: nodeScopes[c.Name], Pool: c.ImagePool}
	}
	images.ManagedSources = compute.NewManagedImageService(volumeRepo, artifactRepo, imageBindings, compute.ImageTransferLimits{MaxBytes: uint64(cfg.ImageTransfers.MaxBytes), Timeout: time.Duration(cfg.ImageTransfers.TimeoutSeconds) * time.Second, IdleTimeout: time.Duration(cfg.ImageTransfers.IdleTimeoutSeconds) * time.Second})

	sessions, err := filesystem.NewAuthRepository(util.ExpandHomeDir(cfg.API.SessionDir), "refresh")
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize refresh-session storage")
	}
	defer sessions.Close()
	tokens, err := filesystem.NewAuthRepository(util.ExpandHomeDir(cfg.API.TokenDir), "personal")
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize personal-token storage")
	}
	defer tokens.Close()
	authentication, err := auth.New(cfg.API.JWTSigningKey, sessions, tokens, cfg.API.AuthPolicy().Eligible)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize authentication")
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := authentication.Cleanup(); err != nil {
				logger.Error().Err(err).Msg("authentication cleanup failed")
			}
		}
	}()
	taskRepo, err := filesystem.NewTaskRepository(util.ExpandHomeDir(cfg.Tasks.Directory))
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize durable task storage")
	}
	defer taskRepo.Close()
	taskService := tasks.New(taskRepo, &compute.CommandHandler{Clouds: clouds, Manager: vmanager, VMs: vms, Volumes: volumes, Nodes: nodes, Keys: keys, Scopes: nodeScopes}, logger.With().Str("component", "tasks").Logger())
	executionContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := taskService.Start(executionContext, cfg.Tasks.Workers); err != nil {
		logger.Fatal().Err(err).Msg("cannot start task executor")
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-executionContext.Done():
				return
			case now := <-ticker.C:
				if err := taskService.Cleanup(now, time.Duration(cfg.Tasks.RetentionDays)*24*time.Hour); err != nil {
					logger.Error().Msg("task history cleanup failed")
				}
			}
		}
	}()
	apiEnv, err := api.New(cfg, logger, network, keys, volpools, nodes, volumes, vms, vmanager, images, authentication, taskService)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize API")
	}
	server := http.Server{
		Addr:    cfg.API.Listen,
		Handler: apiEnv,
	}
	go func() {
		<-executionContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	logger.Info().Str("addr", server.Addr).Msg("starting server")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("serve failed")
		stop()
	}
	stop()
	taskService.Wait()
}
