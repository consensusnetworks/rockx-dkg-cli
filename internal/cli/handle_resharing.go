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

package cli

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/RockX-SG/frost-dkg-demo/internal/messenger"
	"github.com/bloxapp/ssv-spec/dkg"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/urfave/cli/v2"
)

func (h *CliHandler) HandleResharing(c *cli.Context) error {
	resharingRequest := &ResharingRequest{}
	if err := resharingRequest.parseResharingRequest(c); err != nil {
		return fmt.Errorf("HandleResharing: failed to parse resharing request: %w", err)
	}

	requestID := getRandRequestID()
	requestIDInHex := hex.EncodeToString(requestID[:])

	operators := resharingRequest.newOperators()
	operatorsOld := resharingRequest.oldOperators()
	alloperators := append(operators, operatorsOld...)

	messengerClient := messenger.NewMessengerClient(messenger.MessengerAddrFromEnv())
	if err := messengerClient.CreateTopic(requestIDInHex, alloperators); err != nil {
		return fmt.Errorf("HandleResharing: failed to createa new topic on messenger service: %w", err)
	}

	initMsgBytes, err := resharingRequest.initMsgForResharing(requestID)
	if err != nil {
		return fmt.Errorf("HandleResharing: failed to generate init message for keygen: %w", err)
	}

	for _, operatorID := range alloperators {
		addr := resharingRequest.nodeAddress(operatorID)
		if err := h.sendReshareMsg(operatorID, addr, initMsgBytes); err != nil {
			return err
		}
	}

	fmt.Printf("resharing init request sent with ID: %s\n", requestIDInHex)
	return nil
}

func (h *CliHandler) sendReshareMsg(operatorID types.OperatorID, addr string, data []byte) error {
	url := fmt.Sprintf("%s/consume", addr)
	resp, err := h.client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send reshare message with code %d to operator %d", resp.StatusCode, operatorID)
	}
	return nil
}

type ResharingRequest struct {
	Operators    map[types.OperatorID]string `json:"operators"`
	Threshold    int                         `json:"threshold"`
	ValidatorPK  string                      `json:"validator_pk"`
	OperatorsOld map[types.OperatorID]string `json:"operators_old"`
}

func (request *ResharingRequest) parseResharingRequest(c *cli.Context) error {
	request.Operators = make(map[types.OperatorID]string)
	request.OperatorsOld = make(map[types.OperatorID]string)
	request.Threshold = c.Int("threshold")
	request.ValidatorPK = c.String("validator-pk")

	operatorkv := c.StringSlice("operator")
	for _, op := range operatorkv {
		op = strings.Trim(op, " ")
		pair := strings.Split(op, "=")
		if len(pair) != 2 {
			return fmt.Errorf("operator %s is not in the form of key=value", op)
		}
		opID, err := strconv.Atoi(pair[0])
		if err != nil {
			return err
		}
		request.Operators[types.OperatorID(opID)] = pair[1]
	}

	oldoperatorkv := c.StringSlice("old-operator")
	for _, op := range oldoperatorkv {
		op = strings.Trim(op, " ")
		pair := strings.Split(op, "=")
		if len(pair) != 2 {
			return fmt.Errorf("operator %s is not in the form of key=value", op)
		}
		opID, err := strconv.Atoi(pair[0])
		if err != nil {
			return err
		}
		request.OperatorsOld[types.OperatorID(opID)] = pair[1]
	}
	return nil
}

func (request *ResharingRequest) nodeAddress(operatorID types.OperatorID) string {
	var nodeAddr string
	_, ok := request.Operators[operatorID]
	if ok {
		nodeAddr = request.Operators[operatorID]
	} else {
		nodeAddr = request.OperatorsOld[operatorID]
	}
	return nodeAddr
}

func (request *ResharingRequest) newOperators() []types.OperatorID {
	operators := []types.OperatorID{}
	for operatorID := range request.Operators {
		operators = append(operators, operatorID)
	}
	return operators
}
func (request *ResharingRequest) oldOperators() []types.OperatorID {
	operatorsOld := []types.OperatorID{}
	for operatorID := range request.OperatorsOld {
		operatorsOld = append(operatorsOld, operatorID)
	}
	return operatorsOld
}

func (request *ResharingRequest) initMsgForResharing(requestID dkg.RequestID) ([]byte, error) {
	vk, err := hex.DecodeString(request.ValidatorPK)
	if err != nil {
		return nil, err
	}

	reshare := testingutils.ReshareMessageData(
		request.newOperators(),
		uint16(request.Threshold),
		vk,
		request.oldOperators(),
	)
	reshareBytes, _ := reshare.Encode()

	// TODO: TBD who signs this init msg
	ks := testingutils.TestingResharingKeySet()
	reshareMsg := testingutils.SignDKGMsg(ks.DKGOperators[1].EncryptionKey, 1, &dkg.Message{
		MsgType:    dkg.ReshareMsgType,
		Identifier: requestID,
		Data:       reshareBytes,
	})
	byts, _ := reshareMsg.Encode()

	msg := &types.SSVMessage{
		MsgType: types.DKGMsgType,
		Data:    byts,
	}
	return msg.Encode()
}
