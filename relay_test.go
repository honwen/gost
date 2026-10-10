package gost

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

var relayUsers = []*url.Userinfo{
	nil,
	url.UserPassword("admin", "123456"),
}

func relayRoundtripTCP(targetURL string, data []byte, user *url.Userinfo) error {
	ln, err := TCPListener("")
	if err != nil {
		return err
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		return err
	}

	client := &Client{
		Connector:   RelayConnector(user),
		Transporter: TCPTransporter(),
	}

	server := &Server{
		Listener: ln,
		Handler:  RelayHandler("", UsersHandlerOption(user)),
	}

	go server.Run()
	defer server.Close()

	conn, err := proxyConn(client, server)
	if err != nil {
		return err
	}
	defer conn.Close()

	// NOTE: the relay request is only sent by ConnectContext,
	// the deprecated Connect does nothing.
	conn, err = client.Connector.ConnectContext(context.Background(), conn, "tcp", u.Host)
	if err != nil {
		return err
	}

	return httpRoundtrip(conn, targetURL, data)
}

func relayRoundtripUDP(host string, data []byte, user *url.Userinfo) error {
	ln, err := TCPListener("")
	if err != nil {
		return err
	}

	client := &Client{
		Connector:   RelayConnector(user),
		Transporter: TCPTransporter(),
	}

	server := &Server{
		Listener: ln,
		Handler:  RelayHandler("", UsersHandlerOption(user)),
	}

	go server.Run()
	defer server.Close()

	conn, err := proxyConn(client, server)
	if err != nil {
		return err
	}
	defer conn.Close()

	conn, err = client.Connector.ConnectContext(context.Background(), conn, "udp", host)
	if err != nil {
		return err
	}

	conn.SetDeadline(time.Now().Add(1 * time.Second))
	defer conn.SetDeadline(time.Time{})

	if _, err = conn.Write(data); err != nil {
		return err
	}

	recv := make([]byte, len(data))
	if _, err = conn.Read(recv); err != nil {
		return err
	}

	if string(recv) != string(data) {
		return fmt.Errorf("data not equal")
	}

	return nil
}

func TestRelayTCP(t *testing.T) {
	httpSrv := httptest.NewServer(httpTestHandler)
	defer httpSrv.Close()

	sendData := make([]byte, 128)
	rand.Read(sendData)

	for i, tc := range relayUsers {
		tc := tc
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			if err := relayRoundtripTCP(httpSrv.URL, sendData, tc); err != nil {
				t.Errorf("#%d got error: %v", i, err)
			}
		})
	}
}

func TestRelayUDP(t *testing.T) {
	udpSrv := newUDPTestServer(udpTestHandler)
	udpSrv.Start()
	defer udpSrv.Close()

	sendData := make([]byte, 128)
	rand.Read(sendData)

	for i, tc := range relayUsers {
		tc := tc
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			if err := relayRoundtripUDP(udpSrv.Addr(), sendData, tc); err != nil {
				t.Errorf("#%d got error: %v", i, err)
			}
		})
	}
}
