// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// streamClusterInfo is the cluster sub-object returned by
// $JS.API.STREAM.INFO. Only the fields the conformance tests need
// are decoded; the server may return more.
type streamClusterInfo struct {
	Name     string                `json:"name,omitempty"`
	Leader   string                `json:"leader,omitempty"`
	Replicas []streamReplicaStatus `json:"replicas,omitempty"`
}

type streamReplicaStatus struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Active  int64  `json:"active"`
}

// streamInfoResp captures the bits of $JS.API.STREAM.INFO this package
// reads. Other fields (full Config, full State) are out of scope here.
type streamInfoResp struct {
	Cluster *streamClusterInfo `json:"cluster,omitempty"`
	State   struct {
		Messages uint64 `json:"messages"`
		FirstSeq uint64 `json:"first_seq"`
		LastSeq  uint64 `json:"last_seq"`
	} `json:"state"`
	Error *apiError `json:"error,omitempty"`
}

// streamInfo returns the parsed $JS.API.STREAM.INFO response. Used to
// observe `cluster.leader` for stepdown tests.
func streamInfo(h *harness.Harness, name string) (*streamInfoResp, error) {
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var out streamInfoResp
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, fmt.Errorf("decode stream info: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("stream info error: %s", out.Error)
	}
	return &out, nil
}

// streamLeader returns the current cluster leader name for the stream,
// or "" if the stream is not clustered (R1) or no leader has settled.
func streamLeader(h *harness.Harness, name string) (string, error) {
	info, err := streamInfo(h, name)
	if err != nil {
		return "", err
	}
	if info.Cluster == nil {
		return "", nil
	}
	return info.Cluster.Leader, nil
}

// stepDownLeader asks the server to step down the current leader of
// the stream. The new leader is whichever replica wins the election;
// callers should poll streamLeader() until it changes.
func stepDownLeader(h *harness.Harness, name string) error {
	resp, err := h.NC.Request("$JS.API.STREAM.LEADER.STEPDOWN."+name, nil, 5*time.Second)
	if err != nil {
		return err
	}
	var out struct {
		Success bool      `json:"success"`
		Error   *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return fmt.Errorf("decode stepdown resp: %w", err)
	}
	if out.Error != nil {
		return fmt.Errorf("stepdown error: %s", out.Error)
	}
	return nil
}

// awaitLeader returns the current leader once it is non-empty, or "" on
// timeout. Useful at stream-creation time to wait for the initial
// election to settle before publishing.
func awaitLeader(h *harness.Harness, name string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l, err := streamLeader(h, name); err == nil && l != "" {
			return l
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

// awaitLeaderChange polls stream info until the leader differs from
// oldLeader (and is non-empty). Returns the new leader, or "" on
// timeout. The polling interval is intentionally relaxed — leader
// election takes time and a tight loop just hammers the server.
func awaitLeaderChange(h *harness.Harness, name, oldLeader string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l, err := streamLeader(h, name); err == nil && l != "" && l != oldLeader {
			return l
		}
		time.Sleep(750 * time.Millisecond)
	}
	return ""
}

// requireR3Stream creates a 3-replica stream and waits for the initial
// leader election. Returns a skip reason (e.g. "single-server target —
// only 1 replica available") if the cluster cannot satisfy R=3.
func requireR3Stream(h *harness.Harness, cfg streamConfig) (string, error) {
	cfg.Replicas = 3
	if err := createStream(h, cfg); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "insufficient") ||
			strings.Contains(msg, "no suitable peers") ||
			strings.Contains(msg, "available peers") ||
			strings.Contains(msg, "replicas") {
			return "cluster does not have 3 peers available: " + err.Error(), nil
		}
		return "", err
	}
	if leader := awaitLeader(h, cfg.Name, 10*time.Second); leader == "" {
		return "stream did not elect a leader within 10s — target may not be clustered", nil
	}
	return "", nil
}