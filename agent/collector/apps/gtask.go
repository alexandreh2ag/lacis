package apps

import (
	"fmt"
	"path"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"go.yaml.in/yaml/v3"
)

// maxGtaskConfig bounds the gtask configuration read: a list of tasks.
const maxGtaskConfig = 1 << 20

// maxGtaskTasks bounds the tasks reported for one launcher.
const maxGtaskTasks = 200

// maxGtaskField bounds an id or an expression: longer, the task is skipped.
const maxGtaskField = 256

// gtaskConfig is what Lacis reads of a gtask configuration
// (github.com/alexandreh2ag/go-task): the ids and cron expressions of the
// scheduled tasks. Their commands, environment and conditions are never
// read: they may hold secrets.
type gtaskConfig struct {
	Scheduled []struct {
		ID   string `yaml:"id"`
		Expr string `yaml:"expr"`
	} `yaml:"scheduled"`
}

// gtaskConfigPath returns the configuration of a "gtask schedule start" or
// "gtask schedule run" command, -c or --config; empty for another command.
func gtaskConfigPath(command string) string {
	fields := strings.Fields(command)
	if len(fields) < 3 || path.Base(fields[0]) != "gtask" || fields[1] != "schedule" ||
		(fields[2] != "start" && fields[2] != "run") {
		return ""
	}
	for i, f := range fields[3:] {
		for _, name := range []string{"-c", "--config"} {
			if v, ok := strings.CutPrefix(f, name+"="); ok {
				return v
			}
			if f == name && 3+i+1 < len(fields) {
				return fields[3+i+1]
			}
		}
	}
	return ""
}

// gtaskTasks returns the scheduled tasks of a launcher running gtask, read
// from its configuration when the file lies in the app, as given or once
// its links resolved; nil otherwise.
func (c *collection) gtaskTasks(a *app, l system.Launcher) ([]snapshot.LauncherTask, error) {
	file := gtaskConfigPath(l.Command)
	if file == "" {
		return nil, nil
	}
	if !path.IsAbs(file) {
		if l.Directory == "" {
			return nil, nil // relative to the directory of supervisord: unknown
		}
		file = path.Join(l.Directory, file)
	}
	root, rel, ok := inApp(a, path.Clean(file))
	if !ok {
		// The file may reach the app through a link: /home/web/var/www/x
		// → /home/web/builds/x. The resolved path is checked the same way,
		// and the read stays confined to the root of the app.
		if resolved, err := c.sys.Resolve(file); err == nil {
			root, rel, ok = inApp(a, resolved)
		}
	}
	if !ok {
		return nil, fmt.Errorf("gtask config %s of %s: out of the app, not read", file, l.Name)
	}
	appFS, closeRoot, err := openRoot(c.ctx.Fs, root)
	if err != nil {
		return nil, fmt.Errorf("gtask config of %s: %w", l.Name, err)
	}
	defer closeRoot()
	b, err := readSmall(appFS, rel, maxGtaskConfig)
	if err != nil {
		return nil, fmt.Errorf("gtask config %s: %w", file, err)
	}
	var cfg gtaskConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("gtask config %s: %w", file, err)
	}
	var tasks []snapshot.LauncherTask
	for _, t := range cfg.Scheduled {
		if t.ID == "" || len(t.ID) > maxGtaskField || len(t.Expr) > maxGtaskField || len(tasks) == maxGtaskTasks {
			continue
		}
		tasks = append(tasks, snapshot.LauncherTask{ID: t.ID, Schedule: t.Expr})
	}
	return tasks, nil
}

// inApp returns the root of the app holding file, as given or resolved,
// and the path of file in it.
func inApp(a *app, file string) (root, rel string, ok bool) {
	for _, r := range []string{a.path, a.resolved} {
		if r == "" {
			continue
		}
		if rel, ok := strings.CutPrefix(file, path.Clean(r)+"/"); ok {
			return r, rel, true
		}
	}
	return "", "", false
}
