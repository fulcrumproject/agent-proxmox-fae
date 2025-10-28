package vmagent

// ProxmoxClient defines the interface for interacting with Proxmox API
type ProxmoxClient interface {
	// Health and connectivity
	Ping() (*PingResponse, error)

	// User and resource management
	CreateUser(req *PveUserCreation) error
	GetUserPools(username string) (*PveUserResources, error)

	// VM management
	CreateVMFromTemplate(username string, vmReq *VmCreateFromTemplate) error
	GetVM(username string, vmid int) (*Vm, error)
	PatchVM(username string, vmid int, patchReq *VmPatch) error
	DeleteVM(username string, vmid int) error
	StartVM(username string, vmid int) error
	StopVM(username string, vmid int) error

	// Firewall management
	AddVMFirewallRule(username string, vmid int, rule *FirewallRule) error
	GetVMFirewallRules(username string, vmid int) ([]FirewallRule, error)
	DeleteVMFirewallRule(username string, vmid int, pos int) error
}

// PingResponse represents the response from the ping endpoint
type PingResponse struct {
	Fae     bool `json:"fae"`
	Proxmox bool `json:"proxmox"`
}

// PveUserCreation represents the request for creating a new PVE user
type PveUserCreation struct {
	UserID string `json:"userid"`
	PoolID string `json:"poolid"`
}

// PveTemplate represents a VM template
type PveTemplate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// PveUserResources represents the resources available to a PVE user
type PveUserResources struct {
	NextVMID  int           `json:"nextvmid"`
	Nodes     []string      `json:"nodes"`
	Pools     []string      `json:"pools"`
	Images    []string      `json:"images"`
	Templates []PveTemplate `json:"templates"`
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

// VmCreateFromTemplate represents the request for creating a new VM from a template
type VmCreateFromTemplate struct {
	Node         string `json:"node"`
	VMID         int    `json:"vmid"`
	TemplateVMID int    `json:"templatevmid"`
	Name         string `json:"name"`
	Pool         string `json:"pool"`
	Sockets      int    `json:"sockets"`
	Cores        int    `json:"cores"`
	Memory       int    `json:"memory"`
	DiskSize     int    `json:"disksize"`
	LoginUser    string `json:"loginuser,omitempty"`
	LoginPass    string `json:"loginpass,omitempty"`
	LoginSSHKey  string `json:"loginsshkey,omitempty"`
	IPAddress    string `json:"ipaddress,omitempty"`
	Netmask      int    `json:"netmask,omitempty"`
	Gateway      string `json:"gateway,omitempty"`
	Nameserver   string `json:"nameserver,omitempty"`
	SearchDomain string `json:"searchdomain,omitempty"`
}

// VmPatch represents the request for patching a VM
type VmPatch struct {
	Cores  int `json:"cores"`
	Memory int `json:"memory"`
}
