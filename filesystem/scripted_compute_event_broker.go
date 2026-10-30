package filesystem

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"github.com/rs/zerolog"
)

type scriptedComputeEventBrokerSubscribtion struct {
	Event     string
	Script    string
	Mandatory bool
}

type ScriptedComputeEventBroker struct {
	logger zerolog.Logger
	subs   []scriptedComputeEventBrokerSubscribtion
}

func NewScriptedComputeEventBroker(logger zerolog.Logger) *ScriptedComputeEventBroker {
	return &ScriptedComputeEventBroker{
		logger: logger,
		subs:   []scriptedComputeEventBrokerSubscribtion{},
	}
}

func (epub *ScriptedComputeEventBroker) Subscribe(event, script string, mandatory bool) {
	epub.subs = append(epub.subs, scriptedComputeEventBrokerSubscribtion{
		Event:     event,
		Script:    script,
		Mandatory: mandatory,
	})
}

func (epub *ScriptedComputeEventBroker) Publish(event compute.Event) error {
	for _, sub := range epub.subs {
		if sub.Event != event.Name() {
			continue
		}
		cmd := exec.Command("sh", "-c", sub.Script)
		env := os.Environ()
		for key, value := range event.Plain() {
			env = append(env, "VMANGO_"+strings.ToUpper(key)+"="+value)
		}
		cmd.Env = env
		epub.logger.Info().
			Str("event", event.Name()).
			Msg("running script")

		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		err := cmd.Run()
		if err != nil {
			if sub.Mandatory {
				return util.NewError(err, "mandatory %s hook failed (output omitted because it may contain secrets)", event.Name())
			}
			epub.logger.Warn().Err(err).
				Str("event", event.Name()).
				Msg("cannot run script")
		}
	}
	return nil
}
