package hardware

// GuestSystemObservation reports persistent XML, not proof of guest behavior.
type GuestSystemObservation struct {
	SMBIOSMode string         `json:"smbios_mode,omitempty"`
	SMBIOS     *SMBIOS        `json:"smbios,omitempty"`
	SystemUUID string         `json:"system_uuid,omitempty"`
	Clock      *ObservedClock `json:"clock,omitempty"`
}
type ObservedClock struct {
	Mode       string          `json:"mode,omitempty"`
	Timezone   string          `json:"timezone,omitempty"`
	Adjustment string          `json:"adjustment,omitempty"`
	Basis      string          `json:"basis,omitempty"`
	Start      uint            `json:"start,omitempty"`
	Timers     []ObservedTimer `json:"timers,omitempty"`
}
type ObservedTimer struct {
	Name       string           `json:"name"`
	Present    string           `json:"present,omitempty"`
	TickPolicy string           `json:"tickpolicy,omitempty"`
	Track      string           `json:"track,omitempty"`
	Mode       string           `json:"mode,omitempty"`
	Frequency  uint64           `json:"frequency,omitempty"`
	CatchUp    *ObservedCatchUp `json:"catchup,omitempty"`
}
type ObservedCatchUp struct {
	Threshold uint `json:"threshold"`
	Slew      uint `json:"slew"`
	Limit     uint `json:"limit"`
}
type ObservedHardware struct {
	Storage      StorageObservation `json:"storage,omitempty"`
	Firmware     FirmwareBinding    `json:"firmware"`
	EffectiveCPU *CPUObservation    `json:"effective_cpu,omitempty"`
	CPU          CPUObservation     `json:"cpu"`
	PlatformBinding
	GuestSystem GuestSystemObservation `json:"guest_system"`
}
