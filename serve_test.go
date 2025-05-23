package fuego

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ans struct {
	Ans string `json:"ans"`
}

func testController(c ContextNoBody) (ans, error) {
	return ans{Ans: "Hello World"}, nil
}

func testControllerWithError(c ContextNoBody) (ans, error) {
	return ans{}, HTTPError{Err: errors.New("error happened!")}
}

type testOutTransformer struct {
	Name     string `json:"name"`
	Password string `json:"ans"`
}

func (t *testOutTransformer) OutTransform(ctx context.Context) error {
	t.Name = "M. " + t.Name
	t.Password = "redacted"
	return nil
}

type testOutTransformerOnNotReceiver struct {
	Name     string `json:"name"`
	Password string `json:"ans"`
}

func (t testOutTransformerOnNotReceiver) OutTransform(ctx context.Context) error {
	t.Name = "M. " + t.Name
	t.Password = "redacted"
	return nil
}

var (
	_ OutTransformer = &testOutTransformer{}
	_ OutTransformer = &testOutTransformerOnNotReceiver{}
)

func testControllerWithOutTransformer(c ContextNoBody) (testOutTransformer, error) {
	return testOutTransformer{Name: "John"}, nil
}

func testControllerWithOutTransformerOnValueReceiver(c ContextNoBody) (testOutTransformerOnNotReceiver, error) {
	return testOutTransformerOnNotReceiver{Name: "John"}, nil
}

func testControllerWithOutTransformerStar(c ContextNoBody) (*testOutTransformer, error) {
	return &testOutTransformer{Name: "John"}, nil
}

func testControllerWithOutTransformerStarError(c ContextNoBody) (*testOutTransformer, error) {
	return nil, HTTPError{Err: errors.New("error happened!")}
}

func testControllerWithOutTransformerStarNil(c ContextNoBody) (*testOutTransformer, error) {
	return nil, nil
}

func testControllerReturningString(c ContextNoBody) (string, error) {
	return "hello world", nil
}

func testControllerReturningPtrToString(c ContextNoBody) (*string, error) {
	s := "hello world"
	return &s, nil
}

type TestRequestBody struct {
	A string
	B int
}

type TestResponseBody struct {
	TestRequestBody
}

func TestHttpHandler(t *testing.T) {
	s := NewServer()

	t.Run("can create std http handler from fuego controller", func(t *testing.T) {
		handler := HTTPHandler(s, testController, BaseRoute{})
		if handler == nil {
			t.Error("handler is nil")
		}
	})

	t.Run("can run http handler from fuego controller", func(t *testing.T) {
		handler := HTTPHandler(s, testController, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, crlf(`{"ans":"Hello World"}`), body)
	})

	t.Run("can handle errors in http handler from fuego controller", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerWithError, BaseRoute{})
		if handler == nil {
			t.Error("handler is nil")
		}

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, crlf(`{"title":"Internal Server Error","status":500}`), body)
	})

	t.Run("can outTransform before serializing a value", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerWithOutTransformer, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, crlf(`{"name":"M. John","ans":"redacted"}`), body)
	})

	t.Run("can outTransform before serializing a pointer value", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerWithOutTransformerStar, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, crlf(`{"name":"M. John","ans":"redacted"}`), body)
	})

	t.Run("can handle errors in outTransform", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerWithOutTransformerStarError, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, crlf(`{"title":"Internal Server Error","status":500}`), body)
	})

	t.Run("can handle nil in outTransform", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerWithOutTransformerStarNil, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		body := w.Body.String()
		require.Equal(t, "null\n", body)
	})

	t.Run("returns correct content-type when returning string", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerReturningString, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		require.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	})

	t.Run("returns correct content-type when returning ptr to string", func(t *testing.T) {
		handler := HTTPHandler(s, testControllerReturningPtrToString, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		req.Header.Set("Accept", "text/plain")
		w := httptest.NewRecorder()
		handler(w, req)

		require.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	})
}

func TestSetStatusBeforeSend(t *testing.T) {
	s := NewServer()

	t.Run("can set status before sending", func(t *testing.T) {
		handler := HTTPHandler(s, func(c ContextNoBody) (ans, error) {
			c.Response().WriteHeader(201)
			return ans{Ans: "Hello World"}, nil
		}, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		require.Equal(t, 201, w.Code)

		body := w.Body.String()
		require.Equal(t, crlf(`{"ans":"Hello World"}`), body)
	})

	t.Run("can set status with the shortcut before sending", func(t *testing.T) {
		handler := HTTPHandler(s, func(c ContextNoBody) (ans, error) {
			c.SetStatus(202)
			return ans{Ans: "Hello World"}, nil
		}, BaseRoute{})

		req := httptest.NewRequest("GET", "/testing", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		require.Equal(t, 202, w.Code)

		body := w.Body.String()
		require.Equal(t, crlf(`{"ans":"Hello World"}`), body)
	})
}

type testRenderer struct{}

func (t testRenderer) Render(w io.Writer) error {
	w.Write([]byte("hello"))
	return nil
}

type testCtxRenderer struct{}

func (t testCtxRenderer) Render(ctx context.Context, w io.Writer) error {
	w.Write([]byte("world"))
	return nil
}

type testErrorRenderer struct{}

func (t testErrorRenderer) Render(w io.Writer) error { return errors.New("cannot render") }

type testCtxErrorRenderer struct{}

func (t testCtxErrorRenderer) Render(ctx context.Context, w io.Writer) error {
	return errors.New("cannot render")
}

func TestServeRenderer(t *testing.T) {
	s := NewServer(
		WithErrorSerializer(func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(500)
			w.Write([]byte("<body><h1>error</h1></body>"))
		}),
	)

	t.Run("can serve renderer", func(t *testing.T) {
		Get(s, "/", func(c ContextNoBody) (Renderer, error) {
			return testRenderer{}, nil
		})
		Get(s, "/error-in-controller", func(c ContextNoBody) (Renderer, error) {
			return nil, errors.New("error")
		})
		Get(s, "/error-in-rendering", func(c ContextNoBody) (Renderer, error) {
			return testErrorRenderer{}, nil
		})

		t.Run("normal return", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
			require.Equal(t, "hello", w.Body.String())
		})

		t.Run("error return", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/error-in-controller", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 500, w.Code)
			require.Equal(t, "<body><h1>error</h1></body>", w.Body.String())
		})

		t.Run("error in rendering", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/error-in-rendering", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 500, w.Code)
			require.Equal(t, "<body><h1>error</h1></body>", w.Body.String())
		})
	})

	t.Run("can serve ctx renderer", func(t *testing.T) {
		Get(s, "/ctx", func(c ContextNoBody) (CtxRenderer, error) {
			return testCtxRenderer{}, nil
		})
		Get(s, "/ctx/error-in-controller", func(c ContextNoBody) (CtxRenderer, error) {
			return nil, errors.New("error")
		})
		Get(s, "/ctx/error-in-rendering", func(c ContextNoBody) (CtxRenderer, error) {
			return testCtxErrorRenderer{}, nil
		})

		t.Run("normal return", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ctx", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
			require.Equal(t, "world", w.Body.String())
		})

		t.Run("error return", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ctx/error-in-controller", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 500, w.Code)
			require.Equal(t, "<body><h1>error</h1></body>", w.Body.String())
		})

		t.Run("error in rendering", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ctx/error-in-rendering", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			require.Equal(t, 500, w.Code)
			require.Equal(t, "<body><h1>error</h1></body>", w.Body.String())
		})
	})
}

func TestServeError(t *testing.T) {
	s := NewServer()

	Get(s, "/ctx/error-in-controller", func(c ContextNoBody) (CtxRenderer, error) {
		return nil, HTTPError{Err: errors.New("error")}
	})

	t.Run("error return, asking for HTML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ctx/error-in-controller", nil)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, req)

		require.Equal(t, 500, w.Code)
		require.Equal(t, "500 Internal Server Error", w.Body.String())
	})
}

func TestIni(t *testing.T) {
	t.Run("can initialize ContextNoBody", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ctx/error-in-rendering", nil)
		w := httptest.NewRecorder()
		ctx := NewNetHTTPContext[any, any](BaseRoute{}, w, req, readOptions{})

		require.NotNil(t, ctx)
		require.NotNil(t, ctx.Request())
		require.NotNil(t, ctx.Response())
	})

	t.Run("can initialize ContextNoBody", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ctx/error-in-rendering", nil)
		w := httptest.NewRecorder()
		ctx := NewNetHTTPContext[any, any](BaseRoute{}, w, req, readOptions{})

		require.NotNil(t, ctx)
		require.NotNil(t, ctx.Request())
		require.NotNil(t, ctx.Response())
	})

	t.Run("can initialize ContextWithBody[string]", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ctx/error-in-rendering", nil)
		w := httptest.NewRecorder()
		ctx := NewNetHTTPContext[any, any](BaseRoute{}, w, req, readOptions{})

		require.NotNil(t, ctx)
		require.NotNil(t, ctx.Request())
		require.NotNil(t, ctx.Response())
	})

	t.Run("can initialize ContextWithBody[struct]", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ctx/error-in-rendering", nil)
		w := httptest.NewRecorder()
		ctx := NewNetHTTPContext[any, any](BaseRoute{}, w, req, readOptions{})

		require.NotNil(t, ctx)
		require.NotNil(t, ctx.Request())
		require.NotNil(t, ctx.Response())
	})
}

// runServer is a helper function to run a server in a goroutine and return a function to stop it.
func runServer(t testing.TB, s *Server) func() {
	t.Helper()

	Get(s, "/test", func(ctx ContextNoBody) (string, error) {
		return "OK", nil
	})

	go func() {
		s.Run()
	}()
	return func() { // stop our test server when we are done
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		if err := s.Server.Shutdown(ctx); err != nil {
			t.Log(err)
		}
		cancel()
	}
}

func TestServer_Run(t *testing.T) {
	// This is not a standard test, it is here to ensure that the server can run.
	// Please do not run this kind of test for your controllers, it is NOT unit testing.
	t.Run("can run server", func(t *testing.T) {
		s := NewServer(WithoutLogger())
		shutdown := runServer(t, s)
		defer shutdown()

		require.Eventually(t, func() bool {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			return w.Body.String() == `OK`
		}, 5*time.Second, 500*time.Millisecond)
	})

	t.Run("can run server WithListener", func(t *testing.T) {
		listener, err := net.Listen("tcp", ":8080")
		require.NoError(t, err)
		s := NewServer(WithListener(listener))
		shutdown := runServer(t, s)
		defer shutdown()

		require.Eventually(t, func() bool {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			s.Mux.ServeHTTP(w, req)

			return w.Body.String() == `OK`
		}, 5*time.Second, 500*time.Millisecond)
	})

	t.Run("invalid address", func(t *testing.T) {
		s := NewServer(
			WithAddr("----:nope"),
		)
		require.Error(t, s.Run())
	})
}

func TestServer_RunTLS(t *testing.T) {
	// This is not a standard test, it is here to ensure that the server can run.
	// Please do not run this kind of test for your controllers, it is NOT unit testing.
	testHelper, err := newTLSTestHelper()
	if err != nil {
		t.Fatal(err)
	}
	testTLSConfig, err := testHelper.getTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	testCertFile, testKeyFile, err := testHelper.getTLSFiles()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(testCertFile)
	defer os.Remove(testKeyFile)

	tt := []struct {
		name      string
		tlsConfig *tls.Config
		certFile  string
		keyFile   string
	}{
		{
			name:      "can run TLS server with TLS config and empty files",
			tlsConfig: testTLSConfig,
		},
		{
			name:     "can run TLS server with TLS files",
			certFile: testCertFile,
			keyFile:  testKeyFile,
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(
				WithoutLogger(),
				WithAddr("localhost:3005"),
			)

			if tc.tlsConfig != nil {
				s.Server.TLSConfig = tc.tlsConfig
			}

			Get(s, "/test", func(ctx ContextNoBody) (string, error) {
				return "OK", nil
			})

			go func() { // start our test server async
				err := s.RunTLS(tc.certFile, tc.keyFile)
				if err != nil {
					t.Log(err)
				}
			}()
			defer func() { // stop our test server when we are done
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				if err := s.Server.Shutdown(ctx); err != nil {
					t.Log(err)
				}
				cancel()
			}()

			require.Eventually(t, func() bool {
				req := httptest.NewRequest("GET", "https://localhost:3005/test", nil)
				w := httptest.NewRecorder()
				s.Mux.ServeHTTP(w, req)

				return w.Body.String() == `OK`
			}, 5*time.Second, 500*time.Millisecond)
		})
	}

	t.Run("invalid address", func(t *testing.T) {
		s := NewServer(
			WithAddr("----:nope"),
		)
		require.Error(t, s.RunTLS("", ""))
	})
}

type tlsTestHelper struct {
	cert []byte
	key  []byte
}

func (h *tlsTestHelper) getTLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair(h.cert, h.key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func (h *tlsTestHelper) getTLSFiles() (string, string, error) {
	certFile, err := os.CreateTemp("", "fuego-test-cert-")
	if err != nil {
		return "", "", err
	}
	defer certFile.Close()

	keyFile, err := os.CreateTemp("", "fuego-test-key-")
	if err != nil {
		return "", "", err
	}
	defer keyFile.Close()

	if _, err := certFile.Write(h.cert); err != nil {
		return "", "", err
	}

	if _, err := keyFile.Write(h.key); err != nil {
		return "", "", err
	}

	return certFile.Name(), keyFile.Name(), nil
}

func newTLSTestHelper() (*tlsTestHelper, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Example Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Minute),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes})
	return &tlsTestHelper{cert: certPEM, key: keyPEM}, nil
}

func TestFlow(t *testing.T) {
	newTestCtx := func(w *httptest.ResponseRecorder, r *http.Request) *netHttpContext[any, any] {
		return NewNetHTTPContext[any, any](
			BaseRoute{},
			w,
			r,
			readOptions{},
		)
	}

	t.Run("base", func(t *testing.T) {
		e := NewEngine()
		w := httptest.NewRecorder()
		ctx := newTestCtx(w, httptest.NewRequest("GET", "/", nil))
		Flow(e, ctx, testController)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, crlf(`{"ans":"Hello World"}`), w.Body.String())
	})
	t.Run("with nil return in ErrorHandler", func(t *testing.T) {
		e := NewEngine(WithErrorHandler(func(err error) error { return nil }))
		w := httptest.NewRecorder()
		ctx := newTestCtx(w, httptest.NewRequest("GET", "/", nil))
		Flow(e, ctx, testControllerWithError)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, crlf(`null`), w.Body.String())
	})
	t.Run("transformOut error on value receiver", func(t *testing.T) {
		e := NewEngine()
		tcs := []struct {
			accept           string
			expectedResponse string
		}{
			{
				accept:           "application/json",
				expectedResponse: crlf(`{"title":"Internal Server Error","status":500}`),
			},
			{
				accept:           "application/xml",
				expectedResponse: "<HTTPError><title>Internal Server Error</title><status>500</status></HTTPError>",
			},
			{
				accept:           "text/html",
				expectedResponse: "500 Internal Server Error",
			},
			{
				accept:           "application/x-yaml",
				expectedResponse: crlf("title: Internal Server Error\nstatus: 500"),
			},
			{
				accept:           "text/plain",
				expectedResponse: "500 Internal Server Error",
			},
		}
		for _, tc := range tcs {
			t.Run(fmt.Sprintf("Content Type %s", tc.accept), func(t *testing.T) {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/", nil)
				r.Header.Set("Accept", tc.accept)
				ctx := newTestCtx(w, r)
				Flow(e, ctx, testControllerWithOutTransformerOnValueReceiver)
				assert.Equal(t, http.StatusInternalServerError, w.Code)
				assert.Equal(t, tc.expectedResponse, w.Body.String())
			})
		}
	})
}
