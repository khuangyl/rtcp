package rtcp

// Author: adwpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#page-5
// 0                   1                   2                   3
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |V=2|P|  FMT=15 |    PT=205     |           length              |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                     SSRC of packet sender                     |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                      SSRC of media source                     |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |      base sequence number     |      packet status count      |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                 reference time                | fb pkt. count |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          packet chunk         |         packet chunk          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// .                                                               .
// .                                                               .
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |         packet chunk          |  recv delta   |  recv delta   |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// .                                                               .
// .                                                               .
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |           recv delta          |  recv delta   | zero padding  |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

// for packet status chunk
const (
	// type of packet status chunk
	typeRunLengthChunk    = 0
	typeStatusVectorChunk = 1

	// len of packet status chunk
	packetStautsChunkLength = 2

	// for Status Vector Chunk
	// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#section-3.1.4
	// if S == typeSymbolSizeOneBit, symbol list will be:
	// typeSymbolListPacketReceived or typeSymbolListPacketNotReceived
	typeSymbolSizeOneBit = 0

	typeSymbolListPacketReceived    = 0
	typeSymbolListPacketNotReceived = 1

	// if S == typeSymbolSizeTwoBit, symbol list will be same as:
	// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#section-3.1.4
	typeSymbolSizeTwoBit = 1
)

// type of packet status symbol and recv delta
const (
	// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#section-3.1.1
	typePacketNotReceived = iota
	typePacketReceivedSmallDelta
	typePacketReceivedLargeDelta
	// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#page-7
	// see Example 2: "packet received, w/o recv delta"
	typePacketReceivedWithoutDelta
)

var _ Packet = (*TransportLayerCC)(nil) // assert is a Packet

var (
	errPacketStatusChunkLength = errors.New("packet status chunk must be 2 bytes")
	errDeltaExceedLimit        = errors.New("delta exceed limit")
)

// packetStatusChunk has two kinds:
// RunLengthChunk and StatusVectorChunk
type iPacketStautsChunk interface {
	Marshal() ([]byte, error)
	Unmarshal(rawPacket []byte) error
}

// RunLengthChunk T=typeRunLengthChunk
// 0                   1
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |T| S |       Run Length        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type RunLengthChunk struct {
	iPacketStautsChunk

	// T = typeRunLengthChunk
	Type uint16

	// S: type of packet status
	// kind: typePacketNotReceived or...
	PacketStatusSymbol uint16

	// RunLength: count of S
	RunLength uint16
}

// Marshal ..
func (r RunLengthChunk) Marshal() ([]byte, error) {
	chunk := make([]byte, 2)

	// append 1 bit '0'
	dst := appendNBitsToUint16(0, 1, 0)

	// append 2 bit PacketStatusSymbol
	dst = appendNBitsToUint16(dst, 2, r.PacketStatusSymbol)

	// append 13 bit RunLength
	dst = appendNBitsToUint16(dst, 13, r.RunLength)

	binary.BigEndian.PutUint16(chunk, dst)
	return chunk, nil
}

// Unmarshal ..
func (r *RunLengthChunk) Unmarshal(rawPacket []byte) error {
	if len(rawPacket) != packetStautsChunkLength {
		return errPacketStatusChunkLength
	}

	// record type
	r.Type = typeRunLengthChunk

	// get PacketStatusSymbol
	// r.PacketStatusSymbol = uint16(rawPacket[0] >> 5 & 0x03)
	r.PacketStatusSymbol = getNBitsFromByte(rawPacket[0], 1, 2)

	// get RunLength
	// r.RunLength = uint16(rawPacket[0]&0x1F)*256 + uint16(rawPacket[1])
	r.RunLength = getNBitsFromByte(rawPacket[0], 3, 5)<<8 + uint16(rawPacket[1])
	return nil
}

// StatusVectorChunk T=typeStatusVecotrChunk
// 0                   1
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |T|S|       symbol list         |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type StatusVectorChunk struct {
	iPacketStautsChunk
	// T = typeRunLengthChunk
	Type uint16

	// typeSymbolSizeOneBit or typeSymbolSizeTwoBit
	SymbolSize uint16

	// when SymbolSize = typeSymbolSizeOneBit, SymbolList is 14*1bit:
	// typeSymbolListPacketReceived or typeSymbolListPacketNotReceived
	// when SymbolSize = typeSymbolSizeTwoBit, SymbolList is 7*2bit:
	// typePacketNotReceived typePacketReceivedSmallDelta typePacketReceivedLargeDelta or typePacketReserved
	SymbolList []uint16
}

// Marshal ..
func (r StatusVectorChunk) Marshal() ([]byte, error) {
	chunk := make([]byte, 2)

	// set T  SymbolSize  and  SymbolList(bit2-7)
	// chunk[0] = 1<<7 + r.SymbolSize<<6 + uint8(r.SymbolList>>8)

	// append 1 bit '1'
	dst := appendNBitsToUint16(0, 1, 1)

	// append 1 bit SymbolSize
	dst = appendNBitsToUint16(dst, 1, r.SymbolSize)

	// append 14 bit SymbolList
	for _, s := range r.SymbolList {
		if r.SymbolSize == typeSymbolSizeOneBit {
			dst = appendNBitsToUint16(dst, 1, s)
		}
		if r.SymbolSize == typeSymbolSizeTwoBit {
			dst = appendNBitsToUint16(dst, 2, s)
		}
	}

	binary.BigEndian.PutUint16(chunk, dst)
	// set SymbolList(bit8-15)
	// chunk[1] = uint8(r.SymbolList) & 0x0f
	return chunk, nil
}

// Unmarshal ..
func (r *StatusVectorChunk) Unmarshal(rawPacket []byte) error {
	if len(rawPacket) != packetStautsChunkLength {
		return errPacketStatusChunkLength
	}

	r.Type = typeStatusVectorChunk
	r.SymbolSize = getNBitsFromByte(rawPacket[0], 1, 1)

	if r.SymbolSize == typeSymbolSizeOneBit {
		for i := uint16(0); i < 6; i++ {
			r.SymbolList = append(r.SymbolList, getNBitsFromByte(rawPacket[0], 2+i, 1))
		}
		for i := uint16(0); i < 8; i++ {
			r.SymbolList = append(r.SymbolList, getNBitsFromByte(rawPacket[1], i, 1))
		}
		return nil
	}
	if r.SymbolSize == typeSymbolSizeTwoBit {
		for i := uint16(0); i < 3; i++ {
			r.SymbolList = append(r.SymbolList, getNBitsFromByte(rawPacket[0], 2+i*2, 2))
		}
		for i := uint16(0); i < 4; i++ {
			r.SymbolList = append(r.SymbolList, getNBitsFromByte(rawPacket[1], i*2, 2))
		}
		return nil
	}

	r.SymbolSize = getNBitsFromByte(rawPacket[0], 2, 6)<<8 + uint16(rawPacket[1])
	return nil
}

const (
	//https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#section-3.1.5
	delta250us = 250
)

// RecvDelta are represented as multiples of 250us
// small delta is 1 byte: [0，63.75]ms = [0, 63750]us = [0, 255]*250us
// big delta is 2 bytes: [-8192.0, 8191.75]ms = [-8192000, 8191750]us = [-32768, 32767]*250us
// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#section-3.1.5
type RecvDelta struct {
	Type uint16
	// us
	Delta int64
}

// Marshal ..
func (r RecvDelta) Marshal() ([]byte, error) {
	delta := r.Delta / delta250us

	//small delta
	if r.Type == typePacketReceivedSmallDelta && delta >= 0 && delta <= math.MaxUint8 {
		deltaChunk := make([]byte, 1)
		deltaChunk[0] = byte(delta)
		return deltaChunk, nil
	}

	//big delta
	if r.Type == typePacketReceivedLargeDelta && delta >= math.MinInt16 && delta <= math.MaxInt16 {
		deltaChunk := make([]byte, 2)
		binary.BigEndian.PutUint16(deltaChunk, uint16(delta))
		return deltaChunk, nil
	}

	//overflow
	return nil, errDeltaExceedLimit
}

// Unmarshal ..
func (r *RecvDelta) Unmarshal(rawPacket []byte) error {
	chunkLen := len(rawPacket)

	// must be 1 or 2 bytes
	if chunkLen != 1 && chunkLen != 2 {
		return errDeltaExceedLimit
	}

	if chunkLen == 1 {
		r.Type = typePacketReceivedSmallDelta
		r.Delta = delta250us * int64(rawPacket[0])
		return nil
	}

	r.Type = typePacketReceivedLargeDelta
	r.Delta = delta250us * int64(binary.BigEndian.Uint16(rawPacket))
	return nil
}

const (
	// the offset after header
	baseSequenceNumberOffset = 8
	packetStatusCountOffset  = 10
	referenceTimeOffset      = 12
	fbPktCountOffset         = 15
	packetChunkOffset        = 16
)

// TransportLayerCC for sender-BWE
// https://tools.ietf.org/html/draft-holmer-rmcat-transport-wide-cc-extensions-01#page-5
type TransportLayerCC struct {
	// header
	Header Header

	// SSRC of sender
	SenderSSRC uint32

	// SSRC of the media source
	MediaSSRC uint32

	// Transport wide sequence of rtp extension
	BaseSequenceNumber uint16

	// PacketStatusCount
	PacketStatusCount uint16

	// ReferenceTime
	ReferenceTime uint32

	// FbPktCount
	FbPktCount uint8

	// PacketChunks
	PacketChunks []iPacketStautsChunk

	// RecvDeltas
	RecvDeltas []*RecvDelta
}

// Header returns the Header associated with this packet.
// func (t *TransportLayerCC) Header() Header {
// return t.Header
// return Header{
// Padding: true,
// Count:   FormatTCC,
// Type:    TypeTransportSpecificFeedback,
// // https://tools.ietf.org/html/rfc4585#page-33
// Length: uint16((t.len() / 4) - 1),
// }
// }

// total bytes with padding
func (t *TransportLayerCC) len() int {
	n := headerLength + packetChunkOffset + len(t.PacketChunks)*2
	for _, d := range t.RecvDeltas {
		delta := d.Delta / delta250us

		// small delta
		if delta >= 0 && delta <= math.MaxUint8 {
			n++
			// big delta
		} else if delta >= math.MinInt16 && delta <= math.MaxInt16 {
			n += 2
		}
	}

	// has padding
	if n%4 != 0 {
		n = (n/4 + 1) * 4
	}

	return n
}

func (t TransportLayerCC) String() string {
	out := fmt.Sprintf("TransportLayerCC:\n\tHeader %v\n", t.Header)
	out += fmt.Sprintf("TransportLayerCC:\n\tSender Ssrc %d\n", t.SenderSSRC)
	out += fmt.Sprintf("\tMedia Ssrc %d\n", t.MediaSSRC)
	out += fmt.Sprintf("\tBase Sequence Number %d\n", t.BaseSequenceNumber)
	out += fmt.Sprintf("\tStatus Count %d\n", t.PacketStatusCount)
	out += fmt.Sprintf("\tReference Time %d\n", t.ReferenceTime)
	out += fmt.Sprintf("\tFeedback Packet Count %d\n", t.FbPktCount)
	out += "\tPacketChunks "
	for _, chunk := range t.PacketChunks {
		out += fmt.Sprintf("%+v ", chunk)
	}
	out += "\n\tRecvDeltas "
	for _, delta := range t.RecvDeltas {
		out += fmt.Sprintf("%+v ", delta)
	}
	out += "\n"
	return out
}

// Marshal encodes the TransportLayerCC in binary
func (t TransportLayerCC) Marshal() ([]byte, error) {
	header, err := t.Header.Marshal()
	if err != nil {
		return nil, err
	}
	payload := make([]byte, t.len()-headerLength)
	binary.BigEndian.PutUint32(payload, t.SenderSSRC)
	binary.BigEndian.PutUint32(payload[4:], t.MediaSSRC)
	binary.BigEndian.PutUint16(payload[baseSequenceNumberOffset:], t.BaseSequenceNumber)
	binary.BigEndian.PutUint16(payload[packetStatusCountOffset:], t.PacketStatusCount)
	ReferenceTimeAndFbPktCount := appendNBitsToUint32(0, 24, t.ReferenceTime)
	ReferenceTimeAndFbPktCount = appendNBitsToUint32(ReferenceTimeAndFbPktCount, 8, uint32(t.FbPktCount))
	binary.BigEndian.PutUint32(payload[referenceTimeOffset:], ReferenceTimeAndFbPktCount)
	dumpBinary(payload)
	for i, chunk := range t.PacketChunks {
		b, err := chunk.Marshal()
		if err == nil {
			copy(payload[packetChunkOffset+i*2:], b)
		}
	}
	dumpBinary(payload)
	for i, delta := range t.RecvDeltas {
		b, err := delta.Marshal()
		if err == nil {
			if delta.Type == typePacketReceivedSmallDelta {
				copy(payload[packetChunkOffset+len(t.PacketChunks)*2+i:], b)
			}
			if delta.Type == typePacketReceivedLargeDelta {
				copy(payload[packetChunkOffset+len(t.PacketChunks)*2+i*2:], b)
			}
		}
	}
	dumpBinary(payload)

	return append(header, payload...), nil
}

// Unmarshal ..
func (t *TransportLayerCC) Unmarshal(rawPacket []byte) error {
	if len(rawPacket) < (headerLength + ssrcLength) {
		return errPacketTooShort
	}

	if err := t.Header.Unmarshal(rawPacket); err != nil {
		return err
	}

	// https://tools.ietf.org/html/rfc4585#page-33
	// header's length + payload's length
	totalLength := 4 * (t.Header.Length + 1)

	if totalLength <= headerLength+packetChunkOffset {
		return errPacketTooShort
	}

	if len(rawPacket) < int(totalLength) {
		return errPacketTooShort
	}

	if t.Header.Type != TypeTransportSpecificFeedback || t.Header.Count != FormatTCC {
		return errWrongType
	}

	t.SenderSSRC = binary.BigEndian.Uint32(rawPacket[headerLength:])
	t.MediaSSRC = binary.BigEndian.Uint32(rawPacket[headerLength+ssrcLength:])
	t.BaseSequenceNumber = binary.BigEndian.Uint16(rawPacket[headerLength+baseSequenceNumberOffset:])
	t.PacketStatusCount = binary.BigEndian.Uint16(rawPacket[headerLength+packetStatusCountOffset:])
	t.ReferenceTime = get24BitsFromBytes(rawPacket[headerLength+referenceTimeOffset : headerLength+referenceTimeOffset+3])
	t.FbPktCount = rawPacket[headerLength+fbPktCountOffset : headerLength+fbPktCountOffset+1][0]

	packetStautsPos := uint16(headerLength + packetChunkOffset)
	for i := uint16(0); i < t.PacketStatusCount; i++ {
		if packetStautsPos > totalLength {
			return errPacketTooShort
		}
		typ := getNBitsFromByte(rawPacket[packetStautsPos : packetStautsPos+1][0], 0, 1)
		var iPacketStauts iPacketStautsChunk
		switch typ {
		case typeRunLengthChunk:
			packetStauts := &RunLengthChunk{Type: typ}
			iPacketStauts = packetStauts
			err := packetStauts.Unmarshal(rawPacket[packetStautsPos : packetStautsPos+2])
			if err != nil {
				return err
			}
			if packetStauts.PacketStatusSymbol == typePacketReceivedSmallDelta ||
				packetStauts.PacketStatusSymbol == typePacketReceivedLargeDelta {
				recvDelta := &RecvDelta{Type: packetStauts.PacketStatusSymbol}
				for j := uint16(0); j < packetStauts.RunLength; j++ {
					t.RecvDeltas = append(t.RecvDeltas, recvDelta)
				}
			}
		case typeStatusVectorChunk:
			packetStauts := &StatusVectorChunk{Type: typ}
			iPacketStauts = packetStauts
			err := packetStauts.Unmarshal(rawPacket[packetStautsPos : packetStautsPos+2])
			if err != nil {
				return err
			}
			if packetStauts.SymbolSize == typeSymbolSizeOneBit {
				for j := 0; j < len(packetStauts.SymbolList); j++ {
					if packetStauts.SymbolList[j] == typePacketReceivedSmallDelta {
						recvDelta := &RecvDelta{Type: typePacketReceivedSmallDelta}
						t.RecvDeltas = append(t.RecvDeltas, recvDelta)
					}
				}
			}
			if packetStauts.SymbolSize == typeSymbolSizeTwoBit {
				for j := 0; j < len(packetStauts.SymbolList); j++ {
					if packetStauts.SymbolList[j] == typePacketReceivedSmallDelta || packetStauts.SymbolList[j] == typePacketReceivedLargeDelta {
						recvDelta := &RecvDelta{Type: packetStauts.SymbolList[j]}
						t.RecvDeltas = append(t.RecvDeltas, recvDelta)
					}
				}
			}
		}
		packetStautsPos += 2
		t.PacketChunks = append(t.PacketChunks, iPacketStauts)
	}

	recvDeltasPos := headerLength + packetChunkOffset + 2*t.PacketStatusCount
	for _, delta := range t.RecvDeltas {
		if recvDeltasPos >= totalLength {
			return errPacketTooShort
		}
		if delta.Type == typePacketReceivedSmallDelta {
			err := delta.Unmarshal(rawPacket[recvDeltasPos : recvDeltasPos+1])
			if err != nil {
				return err
			}
			recvDeltasPos++
		}
		if delta.Type == typePacketReceivedLargeDelta {
			err := delta.Unmarshal(rawPacket[recvDeltasPos : recvDeltasPos+2])
			if err != nil {
				return err
			}
			recvDeltasPos += 2
		}
	}

	return nil
}

// DestinationSSRC returns an array of SSRC values that this packet refers to.
func (t TransportLayerCC) DestinationSSRC() []uint32 {
	return []uint32{t.MediaSSRC}
}
