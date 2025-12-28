package services

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// TestGenerator handles auto-generation of test files
type TestGenerator struct {
	testsDir string
}

// NewTestGenerator creates a new TestGenerator
func NewTestGenerator(testsDir string) *TestGenerator {
	return &TestGenerator{
		testsDir: testsDir,
	}
}

// GenerateTestNames generates test file and function names without creating files
// Returns the test file name and test function name
func (g *TestGenerator) GenerateTestNames(title string) (testFile, testFunc string) {
	testFunc = g.titleToTestFunc(title)
	testFile = g.titleToFileName(title)
	return testFile, testFunc
}

// GenerateTestFile creates a test file for a task
// Returns the test file name and test function name
func (g *TestGenerator) GenerateTestFile(title, acceptanceCriteria string) (testFile, testFunc string, err error) {
	// Generate names from title
	testFunc = g.titleToTestFunc(title)
	testFile = g.titleToFileName(title)

	// Check if file already exists
	filePath := filepath.Join(g.testsDir, testFile)

	if _, err := os.Stat(filePath); err == nil {
		// File exists, append new test function
		return testFile, testFunc, g.appendTestToFile(filePath, testFunc, title, acceptanceCriteria)
	}

	// Create new test file
	content := g.generateTestFileContent(testFunc, title, acceptanceCriteria)

	// Ensure tests directory exists
	if err := os.MkdirAll(g.testsDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create tests directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write test file: %w", err)
	}

	return testFile, testFunc, nil
}

// titleToTestFunc converts a title to a Go test function name
// "User Login" -> "TestUserLogin"
// "Add shopping cart items" -> "TestAddShoppingCartItems"
func (g *TestGenerator) titleToTestFunc(title string) string {
	// Remove non-alphanumeric characters except spaces
	reg := regexp.MustCompile(`[^a-zA-Z0-9\s]`)
	clean := reg.ReplaceAllString(title, "")

	// Split into words and capitalize each
	words := strings.Fields(clean)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}

	return "Test" + strings.Join(words, "")
}

// titleToFileName converts a title to a test file name
// "User Login" -> "user_login_test.go"
func (g *TestGenerator) titleToFileName(title string) string {
	// Remove non-alphanumeric characters except spaces
	reg := regexp.MustCompile(`[^a-zA-Z0-9\s]`)
	clean := reg.ReplaceAllString(title, "")

	// Convert to lowercase and replace spaces with underscores
	words := strings.Fields(strings.ToLower(clean))
	name := strings.Join(words, "_")

	// Ensure it ends with _test.go
	if !strings.HasSuffix(name, "_test") {
		name += "_test"
	}

	return name + ".go"
}

// generateTestFileContent creates the content for a new test file
func (g *TestGenerator) generateTestFileContent(testFunc, title, acceptanceCriteria string) string {
	// Escape acceptance criteria for use in comment
	escapedCriteria := strings.ReplaceAll(acceptanceCriteria, "*/", "* /")

	return fmt.Sprintf(`package tests

import (
	"testing"
)

/*
Task: %s

Acceptance Criteria:
%s
*/

// %s tests the implementation of: %s
//
// This test will FAIL until the feature is implemented.
// Implement the feature to make this test pass!
func %s(t *testing.T) {
	// TODO: Implement this test based on the acceptance criteria above
	//
	// Steps:
	// 1. Set up test prerequisites
	// 2. Execute the functionality being tested
	// 3. Assert the expected outcomes

	t.Fatal("Not implemented yet - implement the feature to make this test pass!")
}
`, title, escapedCriteria, testFunc, title, testFunc)
}

// appendTestToFile adds a new test function to an existing file
func (g *TestGenerator) appendTestToFile(filePath, testFunc, title, acceptanceCriteria string) error {
	// Read existing content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read test file: %w", err)
	}

	// Check if test function already exists
	if strings.Contains(string(content), "func "+testFunc+"(") {
		// Test already exists, generate unique name
		testFunc = testFunc + "_" + generateShortID()
	}

	// Escape acceptance criteria
	escapedCriteria := strings.ReplaceAll(acceptanceCriteria, "*/", "* /")

	// Append new test
	newTest := fmt.Sprintf(`

/*
Task: %s

Acceptance Criteria:
%s
*/

// %s tests the implementation of: %s
func %s(t *testing.T) {
	// TODO: Implement this test based on the acceptance criteria above
	t.Fatal("Not implemented yet - implement the feature to make this test pass!")
}
`, title, escapedCriteria, testFunc, title, testFunc)

	newContent := string(content) + newTest

	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write test file: %w", err)
	}

	return nil
}

// generateShortID generates a short random ID for uniqueness
func generateShortID() string {
	// Use timestamp-based approach for simplicity
	return fmt.Sprintf("%d", os.Getpid()%1000)
}

// SanitizeTitle ensures a title is safe for use in function names
func SanitizeTitle(title string) string {
	var result strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}
