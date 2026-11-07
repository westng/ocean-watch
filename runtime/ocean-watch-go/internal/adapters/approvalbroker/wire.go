package approvalbroker

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

const maxMessage = 64 << 10

type bootstrapMessage struct {
	Endpoint  string `json:"endpoint"`
	Challenge string `json:"challenge"`
}

type helloMessage struct {
	Protocol  string `json:"protocol"`
	Challenge string `json:"challenge"`
}

type promptMessage struct {
	Challenge string           `json:"challenge"`
	Request   approval.Request `json:"request"`
}

type decisionMessage struct {
	Challenge string `json:"challenge"`
	Document  string `json:"document"`
	Decision  string `json:"decision"`
}

func writeMessage(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxMessage {
		return ErrUnavailable
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	_, err = io.Copy(writer, io.MultiReader(bytes.NewReader(length[:]), bytes.NewReader(encoded)))
	return err
}

func readMessage(reader io.Reader, value any) error {
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return ErrUnavailable
	}
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || size > maxMessage {
		return ErrUnavailable
	}
	encoded := make([]byte, int(size))
	if _, err := io.ReadFull(reader, encoded); err != nil {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrUnavailable
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrUnavailable
	}
	return nil
}
