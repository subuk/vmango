package dal

import (
	"bytes"
	"fmt"
	"net"
)

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func ipMaskToInt(raw string) int {
	mask := net.IPMask(net.ParseIP(raw).To4())
	size, _ := mask.Size()
	return size
}

func getFirstSubnetIP(rawSubnetIP, rawSubnetMask string) (string, error) {
	ip, ipnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", rawSubnetIP, ipMaskToInt(rawSubnetMask)))
	if err != nil {
		return "", err
	}
	ip = ip.To4()
	ip[len(ip)-1]++
	return ip.Mask(ipnet.Mask).String(), nil
}

func listIPRange(start, end, rawSubnetIP, rawSubnetMask string) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", rawSubnetIP, ipMaskToInt(rawSubnetMask)))
	if err != nil {
		return nil, err
	}
	startIP := net.ParseIP(start)
	endIP := net.ParseIP(end)

	var result []string
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		if !(bytes.Compare(ip.To16(), startIP.To16()) >= 0 && bytes.Compare(ip.To16(), endIP.To16()) <= 0) {
			continue
		}
		result = append(result, ip.String())
	}
	return result, nil
}
