package models

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/utils/rpc"
)

var (
	CanCountSensorModel = resource.NewModel("bill", "classifier-sensor", "classifier-sensor")
	errUnimplemented    = errors.New("unimplemented")
	fenceRE             = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
)

func init() {
	resource.RegisterComponent(sensor.API, CanCountSensorModel,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newCanCountSensor,
		},
	)
}

// Config just names camera + vision service
type Config struct {
	CameraName     string `json:"camera_name"`
	ClassifierName string `json:"classifier_name"`
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.CameraName == "" {
		return nil, fmt.Errorf("missing camera in config at %q", path)
	}
	if cfg.ClassifierName == "" {
		return nil, fmt.Errorf("missing vision service in config at %q", path)
	}
	return []string{cfg.CameraName, cfg.ClassifierName}, nil
}

type CanCountSensor struct {
	name       resource.Name
	logger     logging.Logger
	cfg        *Config
	cancelFunc func()
	camera     camera.Camera
	classifier vision.Service
}

func newCanCountSensor(
	ctx context.Context,
	deps resource.Dependencies,
	rawConf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	cam, err := camera.FromDependencies(deps, conf.CameraName)
	if err != nil {
		return nil, fmt.Errorf("camera %q not found: %w", conf.CameraName, err)
	}
	vs, err := vision.FromDependencies(deps, conf.ClassifierName)
	if err != nil {
		return nil, fmt.Errorf("vision service %q not found: %w", conf.ClassifierName, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	return &CanCountSensor{
		name:       rawConf.ResourceName(),
		logger:     logger,
		cfg:        conf,
		cancelFunc: cancel,
		camera:     cam,
		classifier: vs,
	}, nil
}

func (s *CanCountSensor) Name() resource.Name         { return s.name }
func (s *CanCountSensor) Close(context.Context) error { s.cancelFunc(); return nil }
func (s *CanCountSensor) NewClientFromConn(ctx context.Context, _ rpc.ClientConn, _ string, _ resource.Name, _ logging.Logger) (sensor.Sensor, error) {
	return nil, errUnimplemented
}

func (s *CanCountSensor) Reconfigure(ctx context.Context, deps resource.Dependencies, newConf resource.Config) error {
	conf, err := resource.NativeConfig[*Config](newConf)
	if err != nil {
		return err
	}
	cam, err := camera.FromDependencies(deps, conf.CameraName)
	if err != nil {
		return fmt.Errorf("camera %q not found: %w", conf.CameraName, err)
	}
	vs, err := vision.FromDependencies(deps, conf.ClassifierName)
	if err != nil {
		return fmt.Errorf("vision service %q not found: %w", conf.ClassifierName, err)
	}
	s.cfg = conf
	s.camera = cam
	s.classifier = vs
	return nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if m := fenceRE.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return s
}

func (s *CanCountSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	// 1) grab a frame
	imgBytes, _, err := s.camera.Image(ctx, "", nil)
	if err != nil {
		return nil, fmt.Errorf("grab camera frame: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// 2) call vision
	results, err := s.classifier.Classifications(ctx, img, 1, nil)
	if err != nil {
		return nil, fmt.Errorf("vision classification: %w", err)
	}
	if len(results) == 0 {
		return nil, errors.New("no classification result returned")
	}

	// 3) sanitize and parse JSON out of the label
	rawLabel := results[0].Label()
	cleaned := stripFences(rawLabel)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from vision service %q: %w", rawLabel, err)
	}

	// 4) return it directly
	return out, nil
}

// DoCommand is required by sensor.Sensor but we don’t support any commands yet.
func (s *CanCountSensor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, errUnimplemented
}
