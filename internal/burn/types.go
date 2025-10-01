package burn

type DiskInfo struct {
	Device     string
	Size       string
	SizeBytes  uint64
	Name       string
	Removable  bool
	Mountpoint string
}
