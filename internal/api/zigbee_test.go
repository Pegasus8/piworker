package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/zigbee"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNetworks struct {
	permitted []int
}

func (f *fakeNetworks) Connections() []string { return []string{"home"} }
func (f *fakeNetworks) Network(name string) (zigbee.Network, bool) {
	if name != "home" {
		return zigbee.Network{}, false
	}
	return zigbee.Network{Connection: "home", BaseTopic: "zigbee2mqtt", Connected: true,
		Devices: []zigbee.DeviceStatus{{Device: zigbee.Device{FriendlyName: "living/plug", Model: "S60ZBTPF", Features: []zigbee.Feature{}}}}}, true
}
func (f *fakeNetworks) PermitJoin(name string, seconds int) error {
	if name != "home" {
		return errors.New("broker connection \"x\" is not defined")
	}
	f.permitted = append(f.permitted, seconds)
	return nil
}

func zigbeeRouter() (*mux.Router, *fakeNetworks) {
	f := &fakeNetworks{}
	r := mux.NewRouter()
	NewZigbeeHandler(f).RegisterRoutes(r)
	return r, f
}

func TestZigbeeNetworks(t *testing.T) {
	r, _ := zigbeeRouter()
	rr, env := doJSON(t, r, http.MethodGet, "/api/zigbee", "")
	require.Equal(t, http.StatusOK, rr.Code)
	nets := env["data"].(map[string]any)["networks"].([]any)
	require.Len(t, nets, 1)
	assert.Equal(t, "home", nets[0].(map[string]any)["connection"])

	rr, env = doJSON(t, r, http.MethodGet, "/api/zigbee/home", "")
	require.Equal(t, http.StatusOK, rr.Code)
	dev := env["data"].(map[string]any)["devices"].([]any)[0].(map[string]any)
	assert.Equal(t, "living/plug", dev["friendlyName"])

	rr, _ = doJSON(t, r, http.MethodGet, "/api/zigbee/other", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestZigbeePermitJoin(t *testing.T) {
	r, f := zigbeeRouter()
	rr, _ := doJSON(t, r, http.MethodPost, "/api/zigbee/home/permit-join", `{"seconds":120}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr, _ = doJSON(t, r, http.MethodPost, "/api/zigbee/home/permit-join", `{"seconds":0}`)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, []int{120, 0}, f.permitted)
	for _, body := range []string{`{"seconds":255}`, `{"seconds":-1}`, `{}`, `nope`} {
		rr, _ = doJSON(t, r, http.MethodPost, "/api/zigbee/home/permit-join", body)
		assert.Equal(t, http.StatusBadRequest, rr.Code, body)
	}
	rr, _ = doJSON(t, r, http.MethodPost, "/api/zigbee/other/permit-join", `{"seconds":10}`)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
