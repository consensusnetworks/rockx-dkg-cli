/*
 * ==================================================================
 *Copyright (C) 2022-2023 Altstake Technology Pte. Ltd. (RockX)
 *This file is part of rockx-dkg-cli <https://github.com/RockX-SG/rockx-dkg-cli>
 *CAUTION: THESE CODES HAVE NOT BEEN AUDITED
 *
 *rockx-dkg-cli is free software: you can redistribute it and/or modify
 *it under the terms of the GNU General Public License as published by
 *the Free Software Foundation, either version 3 of the License, or
 *(at your option) any later version.
 *
 *rockx-dkg-cli is distributed in the hope that it will be useful,
 *but WITHOUT ANY WARRANTY; without even the implied warranty of
 *MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 *GNU General Public License for more details.
 *
 *You should have received a copy of the GNU General Public License
 *along with rockx-dkg-cli. If not, see <http://www.gnu.org/licenses/>.
 *==================================================================
 */

package messenger

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/bloxapp/ssv-spec/dkg"
	"github.com/bloxapp/ssv-spec/types"
)

type Client struct {
	SrvAddr string
	client  *http.Client
}

func NewMessengerClient(srvAddr string) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	if srvAddr == "" {
		srvAddr = "https://dkg-messenger.rockx.com"
	}

	return &Client{
		SrvAddr: srvAddr,
		client:  &http.Client{Transport: tr},
	}
}

func (cl *Client) StreamDKGBlame(blame *dkg.BlameOutput) error {
	requestID := hex.EncodeToString(blame.BlameMessage.Message.Identifier[:])
	data, err := json.Marshal(blame)
	if err != nil {
		return err
	}

	return cl.stream("dkgblame", requestID, data)
}

func (cl *Client) StreamDKGOutput(output map[types.OperatorID]*dkg.SignedOutput) error {
	var requestID string

	// assuming all signed output have same identifier. skipping validation here
	for _, output := range output {
		if output.Data != nil {
			requestID = hex.EncodeToString(output.Data.RequestID[:])
		} else if output.KeySignData != nil {
			requestID = hex.EncodeToString(output.KeySignData.RequestID[:])
		}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	return cl.stream("dkgoutput", requestID, data)
}

func (cl *Client) BroadcastDKGMessage(msg *dkg.SignedMessage) error {
	requestID := hex.EncodeToString(msg.Message.Identifier[:])

	msgBytes, err := msg.Encode()
	if err != nil {
		return err
	}
	ssvMsg := types.SSVMessage{
		MsgType: types.DKGMsgType,
		Data:    msgBytes,
	}
	ssvMsgBytes, _ := ssvMsg.Encode()

	return cl.publish(requestID, ssvMsgBytes)
}

func (cl *Client) RegisterOperatorNode(id, addr string) error {
	numtries := 3
	try := 1

	errors := make([]error, 0)
	for ; try <= numtries; try++ {
		sub := &Subscriber{
			Name:    id,
			SrvAddr: addr,
		}
		byts, _ := json.Marshal(sub)

		url := fmt.Sprintf("%s/register_node?subscribes_to=%s", cl.SrvAddr, DefaultTopic)
		resp, err := cl.client.Post(url, "application/json", bytes.NewReader(byts))
		if err != nil {
			err := fmt.Errorf("failed to make request to messenger: %s", err.Error())
			log.Printf("Error: %s\n", err.Error())
			errors = append(errors, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("failed to register operator of ID %s with the messenger on %d try: %s", sub.Name, try, err.Error())
			log.Printf("Error: %s\n", err.Error())
			errors = append(errors, err)
		} else {
			break
		}
	}

	if try > numtries {
		return fmt.Errorf("failed to register this node even after %d tries with errors %+v", numtries, errors)
	}
	return nil
}

func (cl *Client) publish(topicName string, data []byte) error {
	resp, err := cl.client.Post(fmt.Sprintf("%s/publish?topic_name=%s", cl.SrvAddr, topicName), "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call publish request to messenger")
	}
	return nil
}

func (cl *Client) stream(urlparam string, requestID string, data []byte) error {
	resp, err := cl.client.Post(fmt.Sprintf("%s/stream/%s?request_id=%s", cl.SrvAddr, urlparam, requestID), "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call stream %s request to messenger", urlparam)
	}
	return nil
}

func (cl *Client) CreateTopic(requestID string, l []types.OperatorID) error {
	topic := TopicJSON{
		TopicName:   requestID,
		Subscribers: make([]string, 0),
	}
	for _, operatorID := range l {
		topic.Subscribers = append(topic.Subscribers, strconv.Itoa(int(operatorID)))
	}
	data, _ := json.Marshal(topic)

	resp, err := cl.client.Post(fmt.Sprintf("%s/topics", cl.SrvAddr), "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call createTopic on messenger")
	}
	return nil
}

func (cl *Client) GetTopic(topicName string) (*Topic, error) {
	resp, err := cl.client.Get(fmt.Sprintf("%s/topics/%s", cl.SrvAddr, topicName))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to call createTopic on messenger")
	}
	topic := &Topic{}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return topic, json.Unmarshal(body, topic)
}
