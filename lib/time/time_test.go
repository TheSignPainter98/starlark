package time_test

import (
	"testing"

	"github.com/canonical/starlark/lib/time"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/startest"
)

func TestModuleSafeties(t *testing.T) {
	for name, value := range time.Module.Members {
		builtin, ok := value.(*starlark.Builtin)
		if !ok {
			continue
		}

		if safety, ok := time.Safeties[name]; !ok {
			t.Errorf("builtin time.%s has no safety declaration", name)
		} else if actualSafety := builtin.Safety(); actualSafety != safety {
			t.Errorf("builtin time.%s has incorrect safety: expected %v but got %v", name, safety, actualSafety)
		}
	}
	for name, _ := range time.Safeties {
		if _, ok := time.Module.Members[name]; !ok {
			t.Errorf("no method for safety declaration time.%s", name)
		}
	}
}

func TestMethodSafetiesExist(t *testing.T) {
	for name, _ := range time.TimeMethods {
		if _, ok := time.TimeMethodSafeties[name]; !ok {
			t.Errorf("builtin time.%s has no safety declaration", name)
		}
	}
	for name, _ := range time.TimeMethodSafeties {
		if _, ok := time.TimeMethods[name]; !ok {
			t.Errorf("no method for safety declaration time.%s", name)
		}
	}
}

func TestTimeFromTimestampAllocs(t *testing.T) {
}

func TestTimeIsValidTimezoneAllocs(t *testing.T) {
}

func TestTimeNowAllocs(t *testing.T) {
}

func TestTimeParseDurationAllocs(t *testing.T) {
}

func TestTimeParseTimeAllocs(t *testing.T) {
	parse_time, ok := time.Module.Members["parse_time"]
	if !ok {
		t.Errorf("No such builtin: parse_time")
		return
	}

	t.Run("arg=just-time", func(t *testing.T) {
		st := startest.From(t)
		st.SetMaxAllocs(24)
		st.RunThread(func(thread *starlark.Thread) {
			for i := 0; i < st.N; i++ {
				result, err := starlark.Call(thread, parse_time, starlark.Tuple{starlark.String("1970-01-01T00:00:00.00Z")}, nil)
				if err != nil {
					st.Error(err)
				}
				st.KeepAlive(result)
			}
		})
	})

	t.Run("args=with-format", func(t *testing.T) {
		st := startest.From(t)
		st.SetMaxAllocs(24)
		st.RunThread(func(thread *starlark.Thread) {
			for i := 0; i < st.N; i++ {
				args := starlark.Tuple{
					starlark.String("1970-01-20"),
					starlark.String("2006-01-02"),
				}
				result, err := starlark.Call(thread, parse_time, args, nil)
				if err != nil {
					st.Error(err)
				}
				st.KeepAlive(result)
			}
		})
	})

	t.Run("args=with-location", func(t *testing.T) {
		st := startest.From(t)
		st.SetMaxAllocs(0)
		st.RunThread(func(thread *starlark.Thread) {
			for i := 0; i < st.N; i++ {
				args := starlark.Tuple{
					starlark.String("1998-07-20"),
					starlark.String("2006-01-02"),
					starlark.String("Europe/Prague"),
				}
				result, err := starlark.Call(thread, parse_time, args, nil)
				if err != nil {
					st.Error(err)
				}
				st.KeepAlive(result)
			}
		})
	})
}

func TestTimeTimeAllocs(t *testing.T) {
}
