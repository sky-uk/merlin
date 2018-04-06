package ipvs

import (
	"fmt"
	"sort"
	"syscall"

	"github.com/docker/libnetwork/ipvs"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/types"
)

// Flag values are found in ip_vs.h in ipvsadm.
const (
	ipVsSvcFPersistent      = 0x0001         /* persistent port */
	ipVsSvcFHashed          = 0x0002         /* hashed entry */
	ipVsSvcFOnePacket       = 0x0004         /* one-packet scheduling */
	ipVsSvcFSched1          = 0x0008         /* scheduler flag 1 */
	ipVsSvcFSched2          = 0x0010         /* scheduler flag 2 */
	ipVsSvcFSched3          = 0x0020         /* scheduler flag 3 */
	ipVsSvcFSchedShFallback = ipVsSvcFSched1 /* SH fallback */
	ipVsSvcFSchedShPort     = ipVsSvcFSched2 /* SH use port */
)

var (
	schedulerFlags = map[string]uint32{
		"flag-1": ipVsSvcFSched1,
		"flag-2": ipVsSvcFSched2,
		"flag-3": ipVsSvcFSched3,
	}
	schedulerFlagsInverted map[uint32]string

	forwardingMethods = map[types.ForwardMethod]uint32{
		types.ForwardMethod_ROUTE:  ipvs.ConnectionFlagDirectRoute,
		types.ForwardMethod_MASQ:   ipvs.ConnectionFlagMasq,
		types.ForwardMethod_TUNNEL: ipvs.ConnectionFlagTunnel,
	}
	forwardingMethodsInverted map[uint32]types.ForwardMethod
)

func init() {
	schedulerFlagsInverted = make(map[uint32]string)
	for k, v := range schedulerFlags {
		schedulerFlagsInverted[v] = k
	}
	forwardingMethodsInverted = make(map[uint32]types.ForwardMethod)
	for k, v := range forwardingMethods {
		forwardingMethodsInverted[v] = k
	}
}

func toProtocolBits(protocol types.Protocol) (uint16, error) {
	switch protocol {
	case types.Protocol_TCP:
		return syscall.IPPROTO_TCP, nil
	case types.Protocol_UDP:
		return syscall.IPPROTO_UDP, nil
	default:
		return 0, fmt.Errorf("unknown protocol %q", protocol)
	}
}

func fromProtocolBits(protocol uint16) (types.Protocol, error) {
	switch protocol {
	case syscall.IPPROTO_TCP:
		return types.Protocol_TCP, nil
	case syscall.IPPROTO_UDP:
		return types.Protocol_UDP, nil
	default:
		return 0, fmt.Errorf("unknown protocol %q", protocol)
	}
}

// toFlagBits returns the uint32 bitset for a list of flags.
func toFlagBits(flags []string) uint32 {
	var flagbits uint32
	for _, flag := range flags {
		if b, exists := schedulerFlags[flag]; exists {
			flagbits |= b
		} else {
			log.Warnf("unknown scheduler flag %q, ignoring", flag)
		}
	}
	return flagbits
}

func fromFlagBits(flagbits uint32) []string {
	var flags []string
	for f, v := range schedulerFlagsInverted {
		if flagbits&f != 0 {
			flags = append(flags, v)
		}
	}
	sort.Strings(flags)
	return flags
}
