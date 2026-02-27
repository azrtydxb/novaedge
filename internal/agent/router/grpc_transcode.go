/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package router

import (
	"errors"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)
var (
	errInvalidGRPCMethodFormat = errors.New("invalid gRPC method format")
	errIsNotAServiceDescriptor = errors.New("is not a service descriptor")
	errMethod = errors.New("method")
	errGRPCFrameTooShort = errors.New("gRPC frame too short")
	errGRPCFrameLength = errors.New("gRPC frame length")
)


// grpcToHTTP maps gRPC status codes to HTTP status codes per the gRPC specification.
var grpcToHTTP = map[int]int{
	0:  200, // OK
	1:  499, // Cancelled
	2:  500, // Unknown
	3:  400, // InvalidArgument
	4:  504, // DeadlineExceeded
	5:  404, // NotFound
	6:  409, // AlreadyExists
	7:  403, // PermissionDenied
	8:  429, // ResourceExhausted
	9:  400, // FailedPrecondition
	10: 409, // Aborted
	11: 400, // OutOfRange
	12: 501, // Unimplemented
	13: 500, // Internal
	14: 503, // Unavailable
	15: 500, // DataLoss
	16: 401, // Unauthenticated
}

// httpToGRPC maps HTTP methods to the most likely gRPC status code on error.
// Used when the proxy itself must generate an error before reaching the backend.
var httpToGRPC = map[int]int{
	400: 3,  // InvalidArgument
	401: 16, // Unauthenticated
	403: 7,  // PermissionDenied
	404: 5,  // NotFound
	429: 8,  // ResourceExhausted
	500: 13, // Internal
	501: 12, // Unimplemented
	503: 14, // Unavailable
	504: 4,  // DeadlineExceeded
}

// grpcStatusToHTTP converts a gRPC status code to an HTTP status code.
func grpcStatusToHTTP(code int) int {
	if httpCode, ok := grpcToHTTP[code]; ok {
		return httpCode
	}
	return http.StatusInternalServerError
}

// httpStatusToGRPC converts an HTTP status code to a gRPC status code.
func httpStatusToGRPC(code int) int {
	if grpcCode, ok := httpToGRPC[code]; ok {
		return grpcCode
	}
	return 2 // Unknown
}

// GRPCTranscodeConfig holds configuration for gRPC-JSON transcoding.
type GRPCTranscodeConfig struct {
	// ProtoDescriptors is the binary-encoded FileDescriptorSet.
	ProtoDescriptors []byte
	// Services maps HTTP method+path patterns to gRPC service/method.
	// Format: "POST /v1/users" -> "user.UserService/CreateUser"
	Services map[string]string
}

// GRPCTranscodeMiddleware translates HTTP/JSON requests to gRPC and back.
// It supports unary RPCs: the middleware intercepts matching HTTP/JSON requests,
// converts the JSON body to protobuf binary, adds gRPC framing and headers,
// forwards to the gRPC backend, then strips gRPC framing from the response
// and converts the protobuf binary back to JSON.
type GRPCTranscodeMiddleware struct {
	config     *GRPCTranscodeConfig
	logger     *zap.Logger
	methods    map[string]*methodDescriptor
	fileDescs  []protoreflect.FileDescriptor
	jsonMarsh  protojson.MarshalOptions
	jsonUnmrsh protojson.UnmarshalOptions
}

// methodDescriptor caches the resolved input/output message descriptors for a gRPC method.
type methodDescriptor struct {
	fullName   string
	inputDesc  protoreflect.MessageDescriptor
	outputDesc protoreflect.MessageDescriptor
}

// NewGRPCTranscodeMiddleware creates a new GRPCTranscodeMiddleware with the given configuration.
func NewGRPCTranscodeMiddleware(config *GRPCTranscodeConfig, logger *zap.Logger) *GRPCTranscodeMiddleware {
	m := &GRPCTranscodeMiddleware{
		config:  config,
		logger:  logger.With(zap.String("component", "grpc-transcode")),
		methods: make(map[string]*methodDescriptor),
		jsonMarsh: protojson.MarshalOptions{
			EmitUnpopulated: true,
		},
		jsonUnmrsh: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}

	if err := m.buildDescriptors(); err != nil {
		m.logger.Error("failed to build proto descriptors", zap.Error(err))
	}

	return m
}

// buildDescriptors parses the FileDescriptorSet from the config and resolves
// all gRPC method descriptors referenced in the Services map.
func (m *GRPCTranscodeMiddleware) buildDescriptors() error {
	if len(m.config.ProtoDescriptors) == 0 {
		return nil
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(m.config.ProtoDescriptors, &fds); err != nil {
		return fmt.Errorf("unmarshal FileDescriptorSet: %w", err)
	}

	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return fmt.Errorf("create file registry: %w", err)
	}

	// Index all file descriptors.
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		m.fileDescs = append(m.fileDescs, fd)
		return true
	})

	// Resolve each mapping to method descriptors.
	for pattern, grpcMethod := range m.config.Services {
		md, resolveErr := m.resolveMethod(files, grpcMethod)
		if resolveErr != nil {
			m.logger.Warn("failed to resolve gRPC method",
				zap.String("pattern", pattern),
				zap.String("method", grpcMethod),
				zap.Error(resolveErr),
			)
			continue
		}
		m.methods[pattern] = md
	}

	return nil
}

// resolveMethod looks up a gRPC method by its fully qualified name (e.g. "pkg.Service/Method")
// and returns descriptors for the input and output messages.
func (m *GRPCTranscodeMiddleware) resolveMethod(files *protoregistry.Files, fullName string) (*methodDescriptor, error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: %q, expected service/method", errInvalidGRPCMethodFormat, fullName)
	}

	serviceName := protoreflect.FullName(parts[0])
	methodName := protoreflect.Name(parts[1])

	sd, err := files.FindDescriptorByName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("service %q not found: %w", serviceName, err)
	}

	svcDesc, ok := sd.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a service descriptor", errIsNotAServiceDescriptor, serviceName)
	}

	mtdDesc := svcDesc.Methods().ByName(methodName)
	if mtdDesc == nil {
		return nil, fmt.Errorf("%w: %q not found in service %q", errMethod, methodName, serviceName)
	}

	return &methodDescriptor{
		fullName:   fullName,
		inputDesc:  mtdDesc.Input(),
		outputDesc: mtdDesc.Output(),
	}, nil
}

// Wrap returns an http.Handler that intercepts matching HTTP/JSON requests,
// transcodes them to gRPC, forwards to the backend, and transcodes the
// gRPC response back to JSON.
func (m *GRPCTranscodeMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build the lookup key: "METHOD /path"
		pattern := r.Method + " " + r.URL.Path

		md, ok := m.methods[pattern]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		// Validate content type - accept application/json or no body.
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") {
			m.writeJSONError(w, http.StatusUnsupportedMediaType,
				"unsupported content type, expected application/json")
			return
		}

		m.logger.Debug("transcoding HTTP/JSON to gRPC",
			zap.String("pattern", pattern),
			zap.String("grpc_method", md.fullName),
		)

		// Read the JSON body.
		jsonBody, err := io.ReadAll(r.Body)
		if err != nil {
			m.writeJSONError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		// Convert JSON to protobuf.
		inputMsg := dynamicpb.NewMessage(md.inputDesc)
		if len(jsonBody) > 0 {
			if unmarshalErr := m.jsonUnmrsh.Unmarshal(jsonBody, inputMsg); unmarshalErr != nil {
				m.writeJSONError(w, http.StatusBadRequest,
					fmt.Sprintf("failed to parse JSON: %v", unmarshalErr))
				return
			}
		}

		protoBytes, err := proto.Marshal(inputMsg)
		if err != nil {
			m.writeJSONError(w, http.StatusInternalServerError,
				"failed to encode protobuf")
			return
		}

		// Build gRPC framed message: 1 byte compressed flag + 4 bytes length + data.
		grpcFrame := encodeGRPCFrame(protoBytes)

		// Rewrite the request for the gRPC backend.
		grpcPath := "/" + md.fullName
		r.URL.Path = grpcPath
		r.Body = io.NopCloser(bytes.NewReader(grpcFrame))
		r.ContentLength = int64(len(grpcFrame))
		r.Header.Set("Content-Type", "application/grpc")
		r.Header.Set("Te", "trailers")
		r.Method = http.MethodPost

		// Capture the gRPC response.
		rec := &grpcTranscodeResponseWriter{
			header: make(http.Header),
			body:   &bytes.Buffer{},
		}

		next.ServeHTTP(rec, r)

		// Check for gRPC error in trailers or headers.
		grpcStatusStr := rec.trailer.Get("Grpc-Status")
		if grpcStatusStr == "" {
			grpcStatusStr = rec.header.Get("Grpc-Status")
		}
		if grpcStatusStr != "" {
			grpcStatus, parseErr := strconv.Atoi(grpcStatusStr)
			if parseErr == nil && grpcStatus != 0 {
				grpcMsg := rec.trailer.Get("Grpc-Message")
				if grpcMsg == "" {
					grpcMsg = rec.header.Get("Grpc-Message")
				}
				httpStatus := grpcStatusToHTTP(grpcStatus)
				m.writeJSONError(w, httpStatus, grpcMsg)
				return
			}
		}

		// Decode the gRPC response frame.
		responseBody := rec.body.Bytes()
		responseProto, err := decodeGRPCFrame(responseBody)
		if err != nil {
			m.writeJSONError(w, http.StatusBadGateway,
				fmt.Sprintf("failed to decode gRPC response: %v", err))
			return
		}

		// Convert protobuf to JSON.
		outputMsg := dynamicpb.NewMessage(md.outputDesc)
		if len(responseProto) > 0 {
			if unmarshalErr := proto.Unmarshal(responseProto, outputMsg); unmarshalErr != nil {
				m.writeJSONError(w, http.StatusBadGateway,
					fmt.Sprintf("failed to decode protobuf response: %v", unmarshalErr))
				return
			}
		}

		jsonResponse, err := m.jsonMarsh.Marshal(outputMsg)
		if err != nil {
			m.writeJSONError(w, http.StatusInternalServerError,
				"failed to encode JSON response")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonResponse)
	})
}

// writeJSONError writes a JSON error response with the given HTTP status code and message.
func (m *GRPCTranscodeMiddleware) writeJSONError(w http.ResponseWriter, status int, message string) {
	grpcCode := httpStatusToGRPC(status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%d,"message":%q}`, grpcCode, message)
}

// encodeGRPCFrame wraps a protobuf message in a gRPC length-prefixed frame.
// Format: 1 byte compressed flag (0 = uncompressed) + 4 bytes big-endian length + data.
func encodeGRPCFrame(data []byte) []byte {
	frame := make([]byte, 5+len(data))
	frame[0] = 0 // not compressed
	dataLen := len(data)
	frame[1] = byte(dataLen >> 24)
	frame[2] = byte(dataLen >> 16)
	frame[3] = byte(dataLen >> 8)
	frame[4] = byte(dataLen)
	copy(frame[5:], data)
	return frame
}

// decodeGRPCFrame strips the gRPC 5-byte length-prefixed frame header and returns the payload.
// Returns an empty slice if the body is empty.
func decodeGRPCFrame(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 5 {
		return nil, fmt.Errorf("%w: %d bytes", errGRPCFrameTooShort, len(data))
	}
	// data[0] is the compressed flag (ignored for now).
	msgLen := binary.BigEndian.Uint32(data[1:5])
	if int(msgLen) > len(data)-5 {
		return nil, fmt.Errorf("%w: %d exceeds available data %d", errGRPCFrameLength, msgLen, len(data)-5)
	}
	return data[5 : 5+msgLen], nil
}

// grpcTranscodeResponseWriter captures the gRPC backend response including trailers.
type grpcTranscodeResponseWriter struct {
	header     http.Header
	trailer    http.Header
	body       *bytes.Buffer
	statusCode int
}

func (w *grpcTranscodeResponseWriter) Header() http.Header {
	return w.header
}

func (w *grpcTranscodeResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *grpcTranscodeResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// Trailers returns the trailer header map. This is called by gRPC servers
// to set trailing metadata.
func (w *grpcTranscodeResponseWriter) Trailers() http.Header {
	if w.trailer == nil {
		w.trailer = make(http.Header)
	}
	return w.trailer
}
