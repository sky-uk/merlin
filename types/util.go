package types

import (
	"fmt"
	"sort"
	"strings"

	"github.com/golang/protobuf/ptypes"
)

// SortFlags of the virtual service in place.
func (s *VirtualService) SortFlags() {
	if s.Config != nil && len(s.Config.Flags) > 0 {
		// flags are expected to be in order to compare against local state
		sort.Strings(s.Config.Flags)
	}
}

func (s *VirtualService) PrettyString() string {
	if s == nil {
		return "nil"
	}
	return fmt.Sprintf("%s [%s] [%s]", s.Id, s.GetKey().PrettyString(), s.GetConfig().PrettyString())
}

func (k *VirtualService_Key) PrettyString() string {
	if k == nil {
		return "nil"
	}
	return fmt.Sprintf("%s:%d %s", k.Ip, k.Port, k.Protocol.String())
}

func (c *VirtualService_Config) PrettyString() string {
	if c == nil {
		return "nil"
	}
	return fmt.Sprintf("%s (%v)", c.Scheduler, strings.Join(c.Flags, ","))
}

func (r *RealServer) PrettyString() string {
	if r == nil {
		return "nil"
	}
	return fmt.Sprintf("%s [-> %v] [%v] [%v]", r.ServiceID, r.GetKey().PrettyString(), r.GetConfig().PrettyString(),
		r.GetHealthCheck().PrettyString())
}

func (k *RealServer_Key) PrettyString() string {
	if k == nil {
		return "nil"
	}
	return fmt.Sprintf("%s:%d", k.Ip, k.Port)
}

func (c *RealServer_Config) PrettyString() string {
	if c == nil {
		return "nil"
	}
	return fmt.Sprintf("%v weight:%v", c.Forward, c.Weight.GetValue())
}

func (h *RealServer_HealthCheck) PrettyString() string {
	if h == nil {
		return "nil"
	}
	period, _ := ptypes.Duration(h.Period)
	timeout, _ := ptypes.Duration(h.Timeout)
	return fmt.Sprintf("%s every:%v timeout:%v up:%d down:%d", h.Endpoint.GetValue(), period, timeout,
		h.UpThreshold, h.DownThreshold)
}
