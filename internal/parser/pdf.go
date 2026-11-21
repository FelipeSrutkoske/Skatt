package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrPDFPasswordRequired = errors.New("pdf protegido por senha")
	ErrPDFInvalidPassword  = errors.New("senha do pdf invalida")
	ErrPDFNoText           = errors.New("pdf sem texto extraivel")
)

var pdfToTextRunner = execPDFToText

func execPDFToText(ctx context.Context, pdfPath string, password string) (string, string, error) {
	args := []string{"-layout"}
	if password != "" {
		args = append(args, "-upw", password)
	}
	args = append(args, pdfPath, "-")

	cmd := exec.CommandContext(ctx, "pdftotext", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func ExtractPDFText(ctx context.Context, pdfPath string, password string) (string, error) {
	stdout, stderr, err := pdfToTextRunner(ctx, pdfPath, password)
	if err != nil {
		lowerErr := strings.ToLower(stderr)
		if strings.Contains(lowerErr, "incorrect password") ||
			strings.Contains(lowerErr, "password") ||
			strings.Contains(lowerErr, "encrypted") {
			if password == "" {
				return "", ErrPDFPasswordRequired
			}
			return "", ErrPDFInvalidPassword
		}
		return "", fmt.Errorf("falha ao extrair texto do pdf: %v", err)
	}

	cleaned := strings.TrimSpace(strings.ReplaceAll(stdout, "\f", ""))
	if cleaned == "" {
		return "", ErrPDFNoText
	}
	return stdout, nil
}
