package bastion

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neko233/ssh233-agent-server/internal/audit"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/sshkeys"
	"github.com/neko233/ssh233-agent-server/internal/store"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	audit   *audit.Logger
	hostKey ssh.Signer
	logger  *slog.Logger
	mu      sync.RWMutex
}

func New(cfg *config.Config, st *store.Store, auditLog *audit.Logger) (*Server, error) {
	hostKey, err := loadOrCreateHostKey(cfg.SSH.HostKeyPath)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:     cfg,
		store:   st,
		audit:   auditLog,
		hostKey: hostKey,
		logger:  slog.Default().With("component", "bastion"),
	}, nil
}

func userScope(u *models.User) store.Scope {
	return store.Scope{TenantID: u.TenantID, Root: u.IsRoot()}
}

func parseSSHUser(raw string) (username, tenantSlug string) {
	parts := strings.SplitN(raw, "@", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return raw, "default"
}

func (s *Server) lookupSSHUser(raw string) (*models.User, error) {
	username, tenantSlug := parseSSHUser(raw)
	root, err := s.store.GetRootUser(username)
	if err != nil {
		return nil, err
	}
	if root != nil {
		return root, nil
	}
	tenant, err := s.store.GetTenantBySlug(tenantSlug)
	if err != nil || tenant == nil {
		return nil, fmt.Errorf("unknown tenant")
	}
	return s.store.GetUserByUsername(tenant.ID, username)
}

func (s *Server) ListenAndServe() error {
	srvCfg := &ssh.ServerConfig{
		PasswordCallback:  s.passwordAuth,
		PublicKeyCallback: s.publicKeyAuth,
		BannerCallback: func(c ssh.ConnMetadata) string {
			return s.cfg.SSH.Banner
		},
	}
	srvCfg.AddHostKey(s.hostKey)

	listener, err := net.Listen("tcp", s.cfg.Server.SSHAddr)
	if err != nil {
		return fmt.Errorf("listen ssh: %w", err)
	}
	s.logger.Info("SSH bastion listening", "addr", s.cfg.Server.SSHAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.logger.Error("accept connection", "error", err)
			continue
		}
		go s.handleConn(conn, srvCfg)
	}
}

func (s *Server) handleConn(tcpConn net.Conn, srvCfg *ssh.ServerConfig) {
	defer tcpConn.Close()
	clientIP, _, _ := net.SplitHostPort(tcpConn.RemoteAddr().String())

	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, srvCfg)
	if err != nil {
		s.logger.Debug("handshake failed", "error", err, "client", clientIP)
		return
	}
	defer sshConn.Close()

	user, err := s.lookupSSHUser(sshConn.User())
	if err != nil || user == nil || !user.Enabled {
		return
	}

	s.audit.Log(user.TenantID, "ssh_connect", user.Username, user.ID, "", "", "client="+clientIP, clientIP)
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			go s.handleSession(newChannel, user, clientIP)
		case "direct-tcpip":
			go s.handleDirectTCP(newChannel, user, clientIP)
		default:
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

func (s *Server) handleDirectTCP(newChannel ssh.NewChannel, user *models.User, clientIP string) {
	var payload struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		newChannel.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}

	scope := userScope(user)
	host, _ := s.store.GetHostByName(scope, payload.DestAddr)
	if host == nil {
		host, _ = s.store.GetHostByName(scope, strings.Split(payload.DestAddr, ".")[0])
	}
	if host == nil {
		newChannel.Reject(ssh.ConnectionFailed, "host not registered")
		return
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()
	go ssh.DiscardRequests(requests)

	targetAddr := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
	targetConn, err := net.DialTimeout("tcp", targetAddr, 15*time.Second)
	if err != nil {
		s.audit.Log(host.TenantID, "connect_failed", user.Username, user.ID, host.ID, "", err.Error(), clientIP)
		return
	}
	defer targetConn.Close()

	s.audit.Log(host.TenantID, "tcp_forward", user.Username, user.ID, host.ID, "", targetAddr, clientIP)
	go copySSH(channel, targetConn)
	copySSH(targetConn, channel)
}

type rwConn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}

func copySSH(dst, src rwConn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handleSession(newChannel ssh.NewChannel, user *models.User, clientIP string) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	var targetName string
	for req := range requests {
		switch req.Type {
		case "env":
			if len(req.Payload) >= 4 {
				keyLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
				if len(req.Payload) >= 8+keyLen {
					key := string(req.Payload[4 : 4+keyLen])
					valLen := int(req.Payload[4+keyLen])<<24 | int(req.Payload[5+keyLen])<<16 | int(req.Payload[6+keyLen])<<8 | int(req.Payload[7+keyLen])
					if len(req.Payload) >= 8+keyLen+valLen {
						val := string(req.Payload[8+keyLen : 8+keyLen+valLen])
						if key == "TARGET" {
							targetName = val
						}
					}
				}
			}
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "pty-req", "window-change":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			host, targetUser := s.resolveTarget(user, targetName)
			if host == nil {
				msg := "No target host. Use: ssh -o SendEnv=TARGET user@tenant@bastion\r\n"
				channel.Write([]byte(msg))
				if req.WantReply {
					req.Reply(false, nil)
				}
				return
			}
			sessionID := s.startSession(user, host, targetUser, clientIP)
			if req.WantReply {
				req.Reply(true, nil)
			}
			s.proxyShell(channel, user, host, targetUser, sessionID, clientIP)
			if sessionID != "" {
				s.store.CloseSession(sessionID)
			}
			return
		case "exec":
			payload := string(req.Payload[4:])
			parts := strings.SplitN(payload, " ", 2)
			host, targetUser := s.resolveTarget(user, parts[0])
			cmd := payload
			if len(parts) > 1 {
				cmd = parts[1]
			}
			if req.WantReply {
				req.Reply(true, nil)
			}
			if host == nil {
				channel.Write([]byte("unknown host\r\n"))
				return
			}
			s.execCommand(channel, user, host, targetUser, cmd, clientIP)
			return
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) resolveTarget(user *models.User, name string) (*models.Host, string) {
	if name == "" {
		return nil, ""
	}
	host, _ := s.store.GetHostByName(userScope(user), name)
	if host == nil {
		return nil, ""
	}
	return host, host.Username
}

func (s *Server) startSession(user *models.User, host *models.Host, targetUser, clientIP string) string {
	sess := &models.Session{
		TenantID:   host.TenantID,
		UserID:     user.ID,
		Username:   user.Username,
		HostID:     host.ID,
		HostName:   host.Name,
		TargetAddr: fmt.Sprintf("%s@%s:%d", targetUser, host.Address, host.Port),
		Protocol:   "ssh",
		ClientIP:   clientIP,
	}
	if err := s.store.CreateSession(sess); err != nil {
		return ""
	}
	return sess.ID
}

func (s *Server) proxyShell(channel ssh.Channel, user *models.User, host *models.Host, targetUser, sessionID, clientIP string) {
	targetClient, err := s.connectTarget(user, host, targetUser, "", clientIP)
	if err != nil {
		channel.Write([]byte(fmt.Sprintf("connect failed: %v\r\n", err)))
		s.audit.Log(host.TenantID, "connect_failed", user.Username, user.ID, host.ID, sessionID, err.Error(), clientIP)
		return
	}
	defer targetClient.Close()

	targetSession, err := targetClient.NewSession()
	if err != nil {
		channel.Write([]byte(fmt.Sprintf("session failed: %v\r\n", err)))
		return
	}
	defer targetSession.Close()

	targetSession.Stdout = channel
	targetSession.Stderr = channel
	targetSession.Stdin = channel
	targetSession.RequestPty("xterm-256color", 40, 120, ssh.TerminalModes{
		ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
	})

	s.audit.Log(host.TenantID, "session_start", user.Username, user.ID, host.ID, sessionID,
		fmt.Sprintf("target=%s@%s:%d", targetUser, host.Address, host.Port), clientIP)
	if err := targetSession.Shell(); err != nil {
		channel.Write([]byte(fmt.Sprintf("shell failed: %v\r\n", err)))
		return
	}
	_ = targetSession.Wait()
	s.audit.Log(host.TenantID, "session_end", user.Username, user.ID, host.ID, sessionID, "", clientIP)
}

func (s *Server) execCommand(channel ssh.Channel, user *models.User, host *models.Host, targetUser, cmd, clientIP string) {
	targetClient, err := s.connectTarget(user, host, targetUser, "", clientIP)
	if err != nil {
		channel.Write([]byte(fmt.Sprintf("connect failed: %v\r\n", err)))
		return
	}
	defer targetClient.Close()
	targetSession, err := targetClient.NewSession()
	if err != nil {
		return
	}
	defer targetSession.Close()
	targetSession.Stdout = channel
	targetSession.Stderr = channel
	s.audit.Log(host.TenantID, "exec", user.Username, user.ID, host.ID, "", cmd, clientIP)
	_ = targetSession.Run(cmd)
}

func (s *Server) connectTarget(user *models.User, host *models.Host, targetUser, password, clientIP string) (*ssh.Client, error) {
	addr := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
	if targetUser == "" {
		targetUser = host.Username
	}
	clientCfg := &ssh.ClientConfig{
		User: targetUser, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 15 * time.Second,
	}
	kp, err := s.store.GetHostKeyPair(user.ID, host.ID)
	if err != nil {
		return nil, err
	}
	if kp != nil && kp.Uploaded {
		keyPair, err := sshkeys.ParsePrivateKey(kp.PrivateKey)
		if err == nil {
			clientCfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(keyPair.Signer)}
			client, err := ssh.Dial("tcp", addr, clientCfg)
			if err == nil {
				return client, nil
			}
		}
	}
	if password != "" {
		clientCfg.Auth = []ssh.AuthMethod{ssh.Password(password)}
		client, err := ssh.Dial("tcp", addr, clientCfg)
		if err != nil {
			return nil, err
		}
		go s.ensureKeyUploaded(user, host, keyPairFromStore(kp), client)
		return client, nil
	}
	return nil, fmt.Errorf("no credentials for host %q — login via web UI with password first", host.Name)
}

func keyPairFromStore(kp *models.HostKeyPair) *sshkeys.KeyPair {
	if kp == nil {
		return nil
	}
	p, _ := sshkeys.ParsePrivateKey(kp.PrivateKey)
	return p
}

func (s *Server) ensureKeyUploaded(user *models.User, host *models.Host, existing *sshkeys.KeyPair, client *ssh.Client) {
	stored, _ := s.store.GetHostKeyPair(user.ID, host.ID)
	if stored != nil && stored.Uploaded {
		return
	}
	var keyPair *sshkeys.KeyPair
	var err error
	if existing != nil {
		keyPair = existing
	} else if stored != nil {
		keyPair, _ = sshkeys.ParsePrivateKey(stored.PrivateKey)
	}
	if keyPair == nil {
		keyPair, err = sshkeys.Generate(user.Username + "@" + host.Name)
	}
	if err != nil {
		return
	}
	session, err := client.NewSession()
	if err != nil {
		return
	}
	defer session.Close()
	if err := sshkeys.UploadPublicKey(session, keyPair.PublicKey); err != nil {
		s.audit.Log(host.TenantID, "key_upload_failed", user.Username, user.ID, host.ID, "", err.Error(), "")
		return
	}
	modelKP := &models.HostKeyPair{
		UserID: user.ID, HostID: host.ID, PublicKey: keyPair.PublicKey,
		PrivateKey: keyPair.PrivateKey, Fingerprint: keyPair.Fingerprint, Uploaded: true,
	}
	if stored != nil {
		modelKP.ID = stored.ID
	}
	_ = s.store.SaveHostKeyPair(modelKP)
	s.audit.Log(host.TenantID, "key_uploaded", user.Username, user.ID, host.ID, "", "fp="+keyPair.Fingerprint, "")
}

func (s *Server) passwordAuth(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	user, err := s.lookupSSHUser(c.User())
	if err != nil || user == nil || !user.Enabled {
		return nil, fmt.Errorf("authentication failed")
	}
	if !s.store.VerifyPassword(user, string(pass)) {
		return nil, fmt.Errorf("authentication failed")
	}
	return nil, nil
}

func (s *Server) publicKeyAuth(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	return nil, fmt.Errorf("not supported")
}

func (s *Server) ConnectWithPassword(user *models.User, host *models.Host, password string) (*ssh.Client, error) {
	return s.connectTarget(user, host, host.Username, password, "")
}

func (s *Server) ConnectWithKey(user *models.User, host *models.Host) (*ssh.Client, error) {
	return s.connectTarget(user, host, host.Username, "", "")
}

func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if signer, err := ssh.ParsePrivateKey(data); err == nil {
			return signer, nil
		}
		slog.Warn("invalid host key, regenerating", "path", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, block.Bytes, 0600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(privateKey)
}
