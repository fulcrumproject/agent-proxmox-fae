package vmagent

// ProxmoxClient defines the interface for interacting with Proxmox API
type ProxmoxClient interface {
	// Health and connectivity
	Ping() (*PingResponse, error)

	// User and resource management
	CreateUser(req *PveUserCreation) error
	GetUserPools(username string) (*PveUserResources, error)

	// VM management
	CreateVM(username string, vmReq *VmCreation) error
	GetVM(username string, vmid int) (*Vm, error)
	PatchVM(username string, vmid int, patchReq *VmPatch) error
	DeleteVM(username string, vmid int) error
	StartVM(username string, vmid int) error
	StopVM(username string, vmid int) error
}

// PingResponse represents the response from the ping endpoint
type PingResponse struct {
	Fae     bool `json:"fae"`
	Proxmox bool `json:"proxmox"`
}

// PveUserCreation represents the request for creating a new PVE user
type PveUserCreation struct {
	UserID        string `json:"userid"`
	PoolID        string `json:"poolid"`
	ResetIfExists bool   `json:"reset_if_exists"`
}

// PveUserResources represents the resources available to a PVE user
type PveUserResources struct {
	NextID int      `json:"nextid"`
	Nodes  []string `json:"nodes"`
	Pools  []string `json:"pools"`
	IDE2   []string `json:"ide2"`
}

// Vm represents a virtual machine
type Vm struct {
	VMID      int     `json:"vmid"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Node      string  `json:"node"`
	Pool      string  `json:"pool"`
	CPU       float64 `json:"cpu"`
	MaxCPU    float64 `json:"maxcpu"`
	Mem       int64   `json:"mem"`
	MaxMem    int64   `json:"maxmem"`
	Disk      int64   `json:"disk"`
	MaxDisk   int64   `json:"maxdisk"`
	DiskRead  int64   `json:"diskread"`
	DiskWrite int64   `json:"diskwrite"`
	NetIn     int64   `json:"netin"`
	NetOut    int64   `json:"netout"`
	Uptime    int64   `json:"uptime"`
}

// VmCreation represents the request for creating a new VM
type VmCreation struct {
	Node    string `json:"node"`
	VMID    int    `json:"vmid"`
	Name    string `json:"name"`
	Pool    string `json:"pool"`
	Sockets int    `json:"sockets"`
	Cores   int    `json:"cores"`
	Memory  int    `json:"memory"`
	SCSI0   int    `json:"scsi0"` // Disk size in GB
	IDE2    string `json:"ide2"`
}

// VmPatch represents the request for patching a VM
type VmPatch struct {
	Cores  int `json:"cores"`
	Memory int `json:"memory"`
}
