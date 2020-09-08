// Autogenerated by Thrift Compiler (0.9.3)
// DO NOT EDIT UNLESS YOU ARE SURE THAT YOU KNOW WHAT YOU ARE DOING

package jaeger

import (
	"bytes"
	"context"
	"fmt"

	"github.com/apache/thrift/lib/go/thrift"
)

// (needed to ensure safety because of naive import list construction.)
var _ = thrift.ZERO
var _ = fmt.Printf
var _ = bytes.Equal

type Agent interface {
	// Parameters:
	//  - Batch
	EmitBatch(batch *Batch) (err error)
}

type AgentClient struct {
	Transport       thrift.TTransport
	ProtocolFactory thrift.TProtocolFactory
	InputProtocol   thrift.TProtocol
	OutputProtocol  thrift.TProtocol
	SeqId           int32
}

func NewAgentClientFactory(t thrift.TTransport, f thrift.TProtocolFactory) *AgentClient {
	return &AgentClient{Transport: t,
		ProtocolFactory: f,
		InputProtocol:   f.GetProtocol(t),
		OutputProtocol:  f.GetProtocol(t),
		SeqId:           0,
	}
}

func NewAgentClientProtocol(t thrift.TTransport, iprot thrift.TProtocol, oprot thrift.TProtocol) *AgentClient {
	return &AgentClient{Transport: t,
		ProtocolFactory: nil,
		InputProtocol:   iprot,
		OutputProtocol:  oprot,
		SeqId:           0,
	}
}

// Parameters:
//  - Batch
func (p *AgentClient) EmitBatch(batch *Batch) (err error) {
	if err = p.sendEmitBatch(batch); err != nil {
		return
	}
	return
}

func (p *AgentClient) sendEmitBatch(batch *Batch) (err error) {
	oprot := p.OutputProtocol
	if oprot == nil {
		oprot = p.ProtocolFactory.GetProtocol(p.Transport)
		p.OutputProtocol = oprot
	}
	p.SeqId++
	if err = oprot.WriteMessageBegin("emitBatch", thrift.ONEWAY, p.SeqId); err != nil {
		return
	}
	args := AgentEmitBatchArgs{
		Batch: batch,
	}
	if err = args.Write(oprot); err != nil {
		return
	}
	if err = oprot.WriteMessageEnd(); err != nil {
		return
	}
	return oprot.Flush(context.Background())
}

type AgentProcessor struct {
	processorMap map[string]thrift.TProcessorFunction
	handler      Agent
}

func (p *AgentProcessor) AddToProcessorMap(key string, processor thrift.TProcessorFunction) {
	p.processorMap[key] = processor
}

func (p *AgentProcessor) GetProcessorFunction(key string) (processor thrift.TProcessorFunction, ok bool) {
	processor, ok = p.processorMap[key]
	return processor, ok
}

func (p *AgentProcessor) ProcessorMap() map[string]thrift.TProcessorFunction {
	return p.processorMap
}

func NewAgentProcessor(handler Agent) *AgentProcessor {

	self0 := &AgentProcessor{handler: handler, processorMap: make(map[string]thrift.TProcessorFunction)}
	self0.processorMap["emitBatch"] = &agentProcessorEmitBatch{handler: handler}
	return self0
}

func (p *AgentProcessor) Process(iprot, oprot thrift.TProtocol) (success bool, err thrift.TException) {
	ctx := context.Background()
	name, _, seqId, err := iprot.ReadMessageBegin()
	if err != nil {
		return false, err
	}
	if processor, ok := p.GetProcessorFunction(name); ok {
		return processor.Process(ctx, seqId, iprot, oprot)
	}
	iprot.Skip(thrift.STRUCT)
	iprot.ReadMessageEnd()
	x1 := thrift.NewTApplicationException(thrift.UNKNOWN_METHOD, "Unknown function "+name)
	oprot.WriteMessageBegin(name, thrift.EXCEPTION, seqId)
	x1.Write(oprot)
	oprot.WriteMessageEnd()
	oprot.Flush(ctx)
	return false, x1
}

type agentProcessorEmitBatch struct {
	handler Agent
}

func (p *agentProcessorEmitBatch) Process(ctx context.Context, seqId int32, iprot, oprot thrift.TProtocol) (success bool, err thrift.TException) {
	args := AgentEmitBatchArgs{}
	if err = args.Read(iprot); err != nil {
		iprot.ReadMessageEnd()
		return false, err
	}

	iprot.ReadMessageEnd()
	var err2 error
	if err2 = p.handler.EmitBatch(args.Batch); err2 != nil {
		return true, err2
	}
	return true, nil
}

// HELPER FUNCTIONS AND STRUCTURES

// Attributes:
//  - Batch
type AgentEmitBatchArgs struct {
	Batch *Batch `thrift:"batch,1" json:"batch"`
}

func NewAgentEmitBatchArgs() *AgentEmitBatchArgs {
	return &AgentEmitBatchArgs{}
}

var AgentEmitBatchArgs_Batch_DEFAULT *Batch

func (p *AgentEmitBatchArgs) GetBatch() *Batch {
	if !p.IsSetBatch() {
		return AgentEmitBatchArgs_Batch_DEFAULT
	}
	return p.Batch
}
func (p *AgentEmitBatchArgs) IsSetBatch() bool {
	return p.Batch != nil
}

func (p *AgentEmitBatchArgs) Read(iprot thrift.TProtocol) error {
	if _, err := iprot.ReadStructBegin(); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T read error: ", p), err)
	}

	for {
		_, fieldTypeId, fieldId, err := iprot.ReadFieldBegin()
		if err != nil {
			return thrift.PrependError(fmt.Sprintf("%T field %d read error: ", p, fieldId), err)
		}
		if fieldTypeId == thrift.STOP {
			break
		}
		switch fieldId {
		case 1:
			if err := p.readField1(iprot); err != nil {
				return err
			}
		default:
			if err := iprot.Skip(fieldTypeId); err != nil {
				return err
			}
		}
		if err := iprot.ReadFieldEnd(); err != nil {
			return err
		}
	}
	if err := iprot.ReadStructEnd(); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T read struct end error: ", p), err)
	}
	return nil
}

func (p *AgentEmitBatchArgs) readField1(iprot thrift.TProtocol) error {
	p.Batch = &Batch{}
	if err := p.Batch.Read(iprot); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T error reading struct: ", p.Batch), err)
	}
	return nil
}

func (p *AgentEmitBatchArgs) Write(oprot thrift.TProtocol) error {
	if err := oprot.WriteStructBegin("emitBatch_args"); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T write struct begin error: ", p), err)
	}
	if err := p.writeField1(oprot); err != nil {
		return err
	}
	if err := oprot.WriteFieldStop(); err != nil {
		return thrift.PrependError("write field stop error: ", err)
	}
	if err := oprot.WriteStructEnd(); err != nil {
		return thrift.PrependError("write struct stop error: ", err)
	}
	return nil
}

func (p *AgentEmitBatchArgs) writeField1(oprot thrift.TProtocol) (err error) {
	if err := oprot.WriteFieldBegin("batch", thrift.STRUCT, 1); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T write field begin error 1:batch: ", p), err)
	}
	if err := p.Batch.Write(oprot); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T error writing struct: ", p.Batch), err)
	}
	if err := oprot.WriteFieldEnd(); err != nil {
		return thrift.PrependError(fmt.Sprintf("%T write field end error 1:batch: ", p), err)
	}
	return err
}

func (p *AgentEmitBatchArgs) String() string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("AgentEmitBatchArgs(%+v)", *p)
}
