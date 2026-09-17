//go:build windows

package pagesnap

import (
	"context"
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const supported = true

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const (
	// tcpTableOwnerPIDListener asks for listening sockets with their owner.
	tcpTableOwnerPIDListener = 3
	// Row sizes of MIB_TCPROW_OWNER_PID and MIB_TCP6ROW_OWNER_PID.
	tcp4RowSize = 24
	tcp6RowSize = 56
)

func listenerPIDs(_ context.Context, port int) ([]int, error) {
	seen := map[int]bool{}
	var pids []int
	for _, family := range []struct {
		af              uint32
		rowSize         int
		portOff, pidOff int
	}{
		{windows.AF_INET, tcp4RowSize, 8, 20},
		{windows.AF_INET6, tcp6RowSize, 20, 52},
	} {
		buf, err := extendedTCPTable(family.af)
		if err != nil {
			return nil, err
		}
		if len(buf) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(buf))
		for i := 0; i < n; i++ {
			row := buf[4+i*family.rowSize:]
			if len(row) < family.rowSize {
				break
			}
			// The port is stored in network byte order in the low bytes.
			p := int(row[family.portOff])<<8 | int(row[family.portOff+1])
			pid := int(binary.LittleEndian.Uint32(row[family.pidOff:]))
			if p == port && !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	return pids, nil
}

func extendedTCPTable(af uint32) ([]byte, error) {
	var size uint32
	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, size)
		var p *byte
		if size > 0 {
			p = &buf[0]
		}
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&size)),
			0, uintptr(af), tcpTableOwnerPIDListener, 0)
		switch syscall.Errno(r) {
		case 0:
			return buf, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue
		default:
			return nil, fmt.Errorf("listing listening sockets: %w", syscall.Errno(r))
		}
	}
	return nil, fmt.Errorf("listing listening sockets: the table kept growing")
}
