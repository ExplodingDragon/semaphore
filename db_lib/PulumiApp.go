package db_lib

import (
	"fmt"
	"github.com/ansible-semaphore/semaphore/db"
	"github.com/ansible-semaphore/semaphore/pkg/task_logger"
	"github.com/ansible-semaphore/semaphore/util"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

type PulumiApp struct {
	Logger     task_logger.Logger
	Template   db.Template
	Repository db.Repository
	reader     PulumiReader
}

type PulumiLogger struct {
	logger task_logger.Logger
	reader *PulumiReader
}

func (l *PulumiLogger) Log(msg string) {
	l.logger.Log(msg)
}

func (l *PulumiLogger) Logf(format string, a ...any) {
	l.logger.Logf(format, a...)
}

type PulumiReader struct {
	confirmed bool
	logger    *PulumiLogger
}

func (r *PulumiReader) Read(p []byte) (n int, err error) {
	if r.confirmed {
		copy(p, "\n")
		return 1, nil
	}

	r.logger.SetStatus(task_logger.TaskWaitingConfirmation)

	for {
		time.Sleep(time.Second * 3)
		if r.confirmed {
			break
		}
	}

	copy(p, "yes\n")
	r.logger.SetStatus(task_logger.TaskRunningStatus)
	return 4, nil
}

func (l *PulumiLogger) LogWithTime(now time.Time, msg string) {
	l.logger.LogWithTime(now, msg)
}

func (l *PulumiLogger) LogfWithTime(now time.Time, format string, a ...any) {
	l.logger.LogWithTime(now, fmt.Sprintf(format, a...))
}

func (l *PulumiLogger) LogCmd(cmd *exec.Cmd) {
	l.logger.LogCmd(cmd)
}

func (l *PulumiLogger) SetStatus(status task_logger.TaskStatus) {
	if status == task_logger.TaskConfirmed {
		l.reader.confirmed = true
	}

	l.logger.SetStatus(status)
}

func (t *PulumiApp) makeCmd(command string, args []string, environmentVars *[]string) *exec.Cmd {
	cmd := exec.Command(command, args...) //nolint: gas
	cmd.Dir = t.GetFullPath()

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", util.Config.TmpPath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("PWD=%s", cmd.Dir))

	if environmentVars != nil {
		cmd.Env = append(cmd.Env, *environmentVars...)
	}

	// Remove sensitive env variables from cmd process
	for _, env := range getSensitiveEnvs() {
		cmd.Env = append(cmd.Env, env+"=")
	}

	return cmd
}

func (t *PulumiApp) runCmd(command string, args []string) error {
	cmd := t.makeCmd(command, args, nil)
	t.Logger.LogCmd(cmd)
	return cmd.Run()
}

func (t *PulumiApp) GetFullPath() string {
	return path.Join(t.Repository.GetFullPath(t.Template.ID), strings.TrimPrefix(t.Template.Playbook, "/"))
}

func (t *PulumiApp) SetLogger(logger task_logger.Logger) task_logger.Logger {
	internalLogger := &PulumiLogger{
		logger: logger,
		reader: &t.reader,
	}

	t.reader.logger = internalLogger
	t.Logger = internalLogger
	return internalLogger
}

func (t *PulumiApp) InstallRequirements() error {

	if _, ok := t.Logger.(*PulumiLogger); !ok {
		t.SetLogger(t.Logger)
	}

	cmd := t.makeCmd("pulumi", []string{"stack", "init"}, nil)
	t.Logger.LogCmd(cmd)
	err := cmd.Start()
	if err != nil {
		return err
	}
	return cmd.Wait()
}

func (t *PulumiApp) Run(args []string, environmentVars *[]string, inputs map[string]string, cb func(*os.Process)) error {
	cmd := t.makeCmd("pulumi", args, environmentVars)
	t.Logger.LogCmd(cmd)
	cmd.Stdin = &t.reader
	err := cmd.Start()
	if err != nil {
		return err
	}
	cb(cmd.Process)
	return cmd.Wait()
}