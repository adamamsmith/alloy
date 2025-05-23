// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jaegertracing/jaeger-idl/proto-gen/api_v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/confighttp"

	"github.com/grafana/alloy/internal/component/otelcol/extension/jaeger_remote_sampling/internal/jaegerremotesampling/internal/mocks"
)

func TestMissingClientConfigManagerHTTP(t *testing.T) {
	s, err := NewHTTP(componenttest.NewNopTelemetrySettings(), confighttp.ServerConfig{}, nil)
	assert.Equal(t, errMissingStrategyStore, err)
	assert.Nil(t, s)
}

func TestStartAndStopHTTP(t *testing.T) {
	// prepare
	srvSettings := confighttp.ServerConfig{
		Endpoint: "127.0.0.1:0",
	}
	s, err := NewHTTP(componenttest.NewNopTelemetrySettings(), srvSettings, &mocks.MockCfgMgr{})
	require.NoError(t, err)
	require.NotNil(t, s)

	// test
	assert.NoError(t, s.Start(t.Context(), componenttest.NewNopHost()))
	assert.NoError(t, s.Shutdown(t.Context()))
}

func TestEndpointsAreWired(t *testing.T) {
	testCases := []struct {
		desc     string
		endpoint string
	}{
		{
			desc:     "new",
			endpoint: "/sampling",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// prepare
			s, err := NewHTTP(componenttest.NewNopTelemetrySettings(), confighttp.ServerConfig{}, &mocks.MockCfgMgr{
				GetSamplingStrategyFunc: func(_ context.Context, _ string) (*api_v2.SamplingStrategyResponse, error) {
					return &api_v2.SamplingStrategyResponse{
						ProbabilisticSampling: &api_v2.ProbabilisticSamplingStrategy{
							SamplingRate: 1,
						},
					}, nil
				},
			})
			require.NoError(t, err)
			require.NotNil(t, s)

			srv := httptest.NewServer(s.mux)
			defer func() {
				srv.Close()
			}()

			// test
			resp, err := srv.Client().Get(fmt.Sprintf("%s%s?service=foo", srv.URL, tC.endpoint))
			require.NoError(t, err)

			// verify
			samplingStrategiesBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			resp.Body.Close()

			body := string(samplingStrategiesBytes)
			assert.JSONEq(t, `{"probabilisticSampling":{"samplingRate":1}}`, body)
		})
	}
}

func TestServiceNameIsRequired(t *testing.T) {
	// prepare
	s, err := NewHTTP(componenttest.NewNopTelemetrySettings(), confighttp.ServerConfig{}, &mocks.MockCfgMgr{})
	require.NoError(t, err)
	require.NotNil(t, s)

	rw := httptest.NewRecorder()
	req := &http.Request{
		URL: &url.URL{},
	}

	// test
	s.samplingStrategyHandler(rw, req)

	// verify
	body, _ := io.ReadAll(rw.Body)
	assert.Contains(t, string(body), "'service' parameter must be provided")
}

func TestErrorFromClientConfigManager(t *testing.T) {
	s, err := NewHTTP(componenttest.NewNopTelemetrySettings(), confighttp.ServerConfig{}, &mocks.MockCfgMgr{})
	require.NoError(t, err)
	require.NotNil(t, s)

	s.strategyStore = &mocks.MockCfgMgr{
		GetSamplingStrategyFunc: func(_ context.Context, _ string) (*api_v2.SamplingStrategyResponse, error) {
			return nil, errors.New("some error")
		},
	}

	rw := httptest.NewRecorder()
	req := &http.Request{
		URL: &url.URL{
			RawQuery: "service=foo",
		},
	}

	// test
	s.samplingStrategyHandler(rw, req)

	// verify
	body, _ := io.ReadAll(rw.Body)
	assert.Contains(t, string(body), "failed to get sampling strategy for service")
}
