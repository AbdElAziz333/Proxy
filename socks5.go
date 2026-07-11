package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

// handles a raw TCP connection using the SOCKS5 Protocol
func HandleSOCKS5(clientConn net.Conn) {
	defer clientConn.Close()

	// negotiation / authentication phase
	buf := make([]byte, 257)
	if _, err := io.ReadFull(clientConn, buf[:2]); err != nil {
		return
	}

	// SOCKS version must be 5
	if buf[0] != 0x05 {
		log.Printf("SOCKS5: Invlaid version 0x%x", buf[0])
		return
	}

	numMethods := int(buf[1])
	if _, err := io.ReadFull(clientConn, buf[:numMethods]); err != nil {
		return
	}

	// Reply with No Authentication Required (0x00)
	if _, err := clientConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Request Phase
	if _, err := io.ReadFull(clientConn, buf[:4]); err != nil {
		return
	}

	command := buf[1]
	if command != 0x01 { // CONNECT METHOD
		log.Printf("SOCKS5: Unsupported command 0x%x", command)
		clientConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	addrType := buf[3]
	var targetAddr string

	switch addrType {
	case 0x01: // IPv4
		if _, err := io.ReadFull(clientConn, buf[:4]); err != nil {
			return
		}

		targetAddr = net.IP(buf[:4]).String()
	case 0x03: // Domain Name
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(clientConn, lenBuf); err != nil {
			return
		}
		
		domainLen := int(lenBuf[0])

		// Allocate a dedicated slice for the domain name string
		domainBuf := make([]byte, domainLen)
		if _, err := io.ReadFull(clientConn, domainBuf); err != nil {
			return
		}

		targetAddr = string(domainBuf)
	case 0x04: // IPv6
		if _, err := io.ReadFull(clientConn, buf[:16]); err != nil {
			return
		}
		
		targetAddr = net.IP(buf[:16]).String()
	default:
		return
	}

	// Read Rort safely into a dedicated 2-byte buffer
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(clientConn, portBuf); err != nil {
		return
	}

	port := binary.BigEndian.Uint16(portBuf)
	destTarget := fmt.Sprintf("%s:%d", targetAddr, port)

	log.Printf("SOCKS5 Proxying to: %s", destTarget)

	// Connect to the destination

	destConn, err := net.DialTimeout("tcp", destTarget, 10 * time.Second)
	if err != nil {
		// reply with host unreachable
		log.Printf("SOCKS5 Dial failed to %s: %v", destTarget, err)
		clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0 ,0 ,0, 0})
		return
	}
	defer destConn.Close()

	// Dynamic Connection Success Reply
	// Build a reply that mirrors the incoming connection address type

	var reply []byte
	switch addrType {
	case 0x01, 0x03: // For IPv4 or Domain, return a standard 10-byte IPv4 style
		reply = []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	case 0x04: // For IPv6, return a compliant 22-byte blank IPv6 style address
		reply = make([]byte, 22)
		reply[0] = 0x05 // version
		reply[1] = 0x00 // success
		reply[2] = 0x00	// reserved
		reply[3] = 0x04	// ATYP IPv6
		// bytes 4-19 are 0 (IPv6 layout), 20-21 are 0 (port layout)
	}

	// reply success (0x00)
	// sending dummy BND.ADDR and BND.PORT (0.0.0.0:0)
	if _, err := clientConn.Write(reply); err != nil {
		return
	}

	// data transfer (reusing the tunnel logic)
	tunnel(clientConn, destConn)
}