package link

import (
	"encoding/json"
	"fmt"
	"net/netip"
)

// 信令报文类型。发现与配对都走这几个报文，全部是 UDP 单播/广播上的 JSON。
const (
	// TypeAnnounce 是周期广播，宣告「我在这里、我叫什么、我是什么角色」。
	TypeAnnounce = "announce"
	// TypePairRequest 是主动方（将来的客机）向目标主机发起的配对请求。
	TypePairRequest = "pair_request"
	// TypePairAccept 是目标主机接受了配对，并回传自己的证书指纹。
	TypePairAccept = "pair_accept"
	// TypePairReject 是目标主机拒绝配对。
	TypePairReject = "pair_reject"
	// TypeBye 表示主动方退出共享，或者一方不再想保留这段关系。
	TypeBye = "bye"
)

// MessageVersion 是信令报文格式版本。不兼容的版本直接丢弃。
const MessageVersion = 1

// Message 是一条信令报文。
//
// 一台机器同时是「广播者」和「回复者」：收到 announce 后可能回 pair_accept。
// To 用于把回复定向到某个设备，避免同网段多台机器互相插话。
type Message struct {
	// V 是报文版本。
	V int `json:"v"`
	// Type 是报文类型。
	Type string `json:"type"`
	// ID 是发送方设备 ID。
	ID string `json:"id"`
	// Name 是发送方设备名。
	Name string `json:"name,omitempty"`
	// Role 是发送方当前角色。
	Role Role `json:"role,omitempty"`
	// Fingerprint 是发送方 Deskflow 证书指纹（可能为空：还没生成证书）。
	Fingerprint string `json:"fp,omitempty"`
	// Port 是发送方 Deskflow core 的 TCP 端口。
	Port int `json:"port,omitempty"`
	// To 是收件方设备 ID；为空表示广播。
	To string `json:"to,omitempty"`
	// Reason 是拒绝原因，便于 UI 直接显示。
	Reason string `json:"reason,omitempty"`
}

// Encode 把报文序列化成 UDP payload。
func (m Message) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	return b, nil
}

// Decode 解析 UDP payload。未知版本、未知类型、非法字段一律报错，
// 由调用方丢弃该包（发现是尽力而为的，不能因为一个坏包就崩）。
func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return Message{}, fmt.Errorf("%w: %s", ErrInvalidMessage, err)
	}
	if err := m.Validate(); err != nil {
		return Message{}, err
	}
	return m, nil
}

// Validate 校验报文的结构不变量。
func (m Message) Validate() error {
	if m.V != MessageVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidMessage, m.V)
	}
	switch m.Type {
	case TypeAnnounce:
	case TypePairRequest, TypePairAccept, TypePairReject, TypeBye:
	default:
		return fmt.Errorf("%w: unknown type %q", ErrInvalidMessage, m.Type)
	}
	if !ValidDeviceID(m.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, m.ID)
	}
	if m.Name != "" && len([]rune(m.Name)) > MaxDeviceNameLen {
		return fmt.Errorf("%w: too long", ErrInvalidName)
	}
	if m.Role != "" && !m.Role.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidRole, string(m.Role))
	}
	if m.Fingerprint != "" && !ValidFingerprint(m.Fingerprint) {
		return fmt.Errorf("%w: %q", ErrInvalidFingerprint, m.Fingerprint)
	}
	if m.Port < 0 || m.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, m.Port)
	}
	if m.To != "" && !ValidDeviceID(m.To) {
		return fmt.Errorf("%w: %q", ErrInvalidDeviceID, m.To)
	}
	return nil
}

// Announce 由本机身份构造一条 announce 报文。
func Announce(s Self) Message {
	return Message{
		V:           MessageVersion,
		Type:        TypeAnnounce,
		ID:          s.ID,
		Name:        s.Name,
		Role:        s.Role,
		Fingerprint: s.Fingerprint,
		Port:        s.Port,
	}
}

// PeerFrom 用一条报文 + 来源地址构造/更新对端快照。
func PeerFrom(m Message, from netip.AddrPort) Peer {
	return Peer{
		ID:          m.ID,
		Name:        m.Name,
		Addr:        from,
		Fingerprint: m.Fingerprint,
		Role:        m.Role,
	}
}
