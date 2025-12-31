package config

// Default configuration values
// These are used when settings are not specified in TASKS.md front matter
const (
	// DefaultTasksFileName is the name of the tasks file
	DefaultTasksFileName = "TASKS.md"

	// DefaultStaleThresholdDays is the default number of days for a task to be considered stale
	DefaultStaleThresholdDays = 7

	// DefaultTestCommand is the default test command template
	DefaultTestCommand = "go test -v -count=1 -run ^{testFunc}$ {testPath}"

	// DefaultPassString is the default string indicating test passed
	DefaultPassString = "PASS"

	// DefaultFailString is the default string indicating test failed
	DefaultFailString = "FAIL"

	// DefaultNoTestsString is the default string indicating no tests found
	DefaultNoTestsString = "no tests to run"
)
