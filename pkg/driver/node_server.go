package driver

import (
	"context"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	mount "k8s.io/mount-utils"
)

// MaxVolumesPerNode is the maximum number of volumes a single node may host
const MaxVolumesPerNode int64 = 1024

// NodeStageVolume is called after the volume is attached to the instance, so it can be partitioned, formatted and mounted to a staging path
func (d *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Str("staging_target_path", req.StagingTargetPath).Msg("Request: NodeStageVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodeStageVolume")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a StagingTargetPath to NodeStageVolume")
	}
	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeCapability to NodeStageVolume")
	}

	log.Debug().Str("volume_id", req.VolumeId).Msg("Formatting and mounting volume (staging)")

	// Find the disk attachment location
	attachedDiskPath := d.DiskHotPlugger.PathForVolume(req.VolumeId)
	if attachedDiskPath == "" {
		err := status.Error(codes.NotFound, "path to volume (/dev/disk/by-id/VOLUME_ID) not found")
		log.Error().Str("volume_id", req.VolumeId).Err(err)
		return nil, err
	}

	// Format the volume if not already formatted
	formatted, err := d.DiskHotPlugger.IsFormatted(attachedDiskPath)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("volume_id", req.VolumeId).Bool("formatted", formatted).Msg("Is currently formatted?")

	if !formatted {
		d.DiskHotPlugger.Format(d.DiskHotPlugger.PathForVolume(req.VolumeId), "ext4")
	}

	// Mount the volume if not already mounted
	mounted, err := d.DiskHotPlugger.IsMounted(d.DiskHotPlugger.PathForVolume(req.VolumeId))
	if err != nil {
		return nil, err
	}
	log.Debug().Str("volume_id", req.VolumeId).Bool("mounted", formatted).Msg("Is currently mounted?")

	if !mounted {
		mount := req.VolumeCapability.GetMount()
		options := []string{}
		if mount != nil {
			options = mount.MountFlags
		}
		d.DiskHotPlugger.Mount(d.DiskHotPlugger.PathForVolume(req.VolumeId), req.StagingTargetPath, "ext4", options...)
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unmounts the volume when it's finished with, ready for deletion
func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Str("staging_target_path", req.StagingTargetPath).Msg("Request: NodeUnstageVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodeUnstageVolume")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a StagingTargetPath to NodeUnstageVolume")
	}

	log.Debug().Str("volume_id", req.VolumeId).Str("path", req.StagingTargetPath).Msg("Unmounting volume (unstaging)")
	path := d.DiskHotPlugger.PathForVolume(req.VolumeId)

	if path == "" && !d.TestMode {
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	mounted, err := d.DiskHotPlugger.IsMounted(path)
	if err != nil {
		log.Error().Str("path", path).Err(err).Msg("Mounted check errored")
		return nil, err
	}
	log.Debug().Str("volume_id", req.VolumeId).Bool("mounted", mounted).Msg("Mounted check completed")

	if mounted {
		log.Debug().Str("volume_id", req.VolumeId).Bool("mounted", mounted).Msg("Unmounting")
		d.DiskHotPlugger.Unmount(path)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume bind mounts the staging path into the container
func (d *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Str("staging_target_path", req.StagingTargetPath).Str("target_path", req.TargetPath).Msg("Request: NodePublishVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodePublishVolume")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a StagingTargetPath to NodePublishVolume")
	}
	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a TargetPath to NodePublishVolume")
	}
	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeCapability to NodePublishVolume")
	}

	log.Debug().Str("volume_id", req.VolumeId).Str("from_path", req.StagingTargetPath).Str("to_path", req.TargetPath).Msg("Bind-mounting volume (publishing)")

	err := os.MkdirAll(req.TargetPath, 0o750)
	if err != nil {
		return nil, err
	}

	log.Debug().Str("volume_id", req.VolumeId).Str("targetPath", req.TargetPath).Msg("Ensuring target path exists")
	// Mount the volume if not already mounted
	mounted, err := d.DiskHotPlugger.IsMounted(req.TargetPath)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("volume_id", req.VolumeId).Bool("mounted", mounted).Msg("Checking if currently mounting")

	if !mounted {
		options := []string{
			"bind",
		}
		if req.Readonly {
			options = append(options, "ro")
		}
		d.DiskHotPlugger.Mount(req.StagingTargetPath, req.TargetPath, "ext4", options...)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume removes the bind mount
func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Info().Str("volume_id", req.VolumeId).Str("target_path", req.TargetPath).Msg("Request: NodeUnpublishVolume")

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodeUnpublishVolume")
	}
	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "must provide a TargetPath to NodeUnpublishVolume")
	}

	targetPath := req.GetTargetPath()
	log.Info().Str("volume_id", req.VolumeId).Str("path", targetPath).Msg("Removing bind-mount for volume (unpublishing)")

	mounted, err := d.DiskHotPlugger.IsMounted(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debug().Str("targetPath", targetPath).Msg("targetPath has already been deleted")

			return &csi.NodeUnpublishVolumeResponse{}, nil
		}
		if !mount.IsCorruptedMnt(err) {
			return &csi.NodeUnpublishVolumeResponse{}, err
		}

		mounted = true
	}
	log.Debug().Str("volume_id", req.VolumeId).Bool("mounted", mounted).Msg("Checking if currently mounting")

	if !mounted {
		if err = os.RemoveAll(targetPath); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	err = d.DiskHotPlugger.Unmount(targetPath)
	if err != nil {
		return nil, err
	}

	log.Info().Str("volume_id", req.VolumeId).Str("target_path", targetPath).Msg("Removing target path")
	err = os.Remove(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetInfo returns some identifier (ID, name) for the current node
func (d *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	log.Info().Msg("Request: NodeGetInfo")

	nodeInstanceID, region, err := currentNodeDetails()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Debug().Str("node_id", nodeInstanceID).Str("region", region).Msg("Requested information about node")

	return &csi.NodeGetInfoResponse{
		NodeId:            nodeInstanceID,
		MaxVolumesPerNode: MaxVolumesPerNode,

		// make sure that the driver works on this particular region only
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				"region": region,
			},
		},
	}, nil
}

// NodeGetVolumeStats reports on volume health, but we don't implement it yet
func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeExpandVolume is used to expand the filesystem inside volumes, but we don't support that yet
func (d *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeGetCapabilities returns the capabilities that this node and driver support
func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	// Intentionally don't return VOLUME_CONDITION and NODE_GET_VOLUME_STATS
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

type civostatsdConfig struct {
	Server     string
	Token      string
	Region     string
	InstanceID string `toml:"instance_id"`
}

func currentNodeDetails() (string, string, error) {
	configFile := "/etc/civostatsd"

	_, err := os.Stat(configFile)
	if err != nil {
		log.Debug().Msg("Node details file /etc/civostatsd doesn't existing, using ENVironment variables")
		return currentNodeDetailsFromEnv()
	}

	var config civostatsdConfig
	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		log.Debug().Msg("Node details file /etc/civostatsd isn't valid TOML, using ENVironment variables")
		return currentNodeDetailsFromEnv()
	}

	return config.InstanceID, config.Region, nil
}

func currentNodeDetailsFromEnv() (string, string, error) {
	return os.Getenv("NODE_ID"), os.Getenv("REGION"), nil
}
