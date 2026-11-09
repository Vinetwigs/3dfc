package archive

const (
	Magic            = "3DFC"
	Version   uint16 = 1
	HeaderLen        = 40

	PacketGoI byte = 0
	PacketGoP byte = 1
	PacketFF  byte = 2
	PacketRaw byte = 3

	FooterSize   = 16
	IndexRecSize = 24

	FlagKey   = 1 << 0
	FlagFF    = 1 << 1
	FlagRaw   = 1 << 2
	FlagAlpha = 1 << 0
)
