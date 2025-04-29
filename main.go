package main

import (
	"classifier-sensor/models"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	// Register your CanCountSensorModel here
	module.ModularMain(
		resource.APIModel{sensor.API, models.CanCountSensorModel},
	)
}
