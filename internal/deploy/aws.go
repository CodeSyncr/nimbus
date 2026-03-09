package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func deployAWS(dir string, cfg *Config) error {
	// AWS: build, push to ECR, deploy to ECS/App Runner
	// For now, we build the image and provide instructions
	appName := filepath.Base(dir)
	if cfg != nil && cfg.AppName != "" {
		appName = cfg.AppName
	}
	fmt.Println("  Building Docker image for AWS...")
	cmd := exec.Command("docker", "build", "-t", appName+":latest", ".")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	fmt.Println()
	fmt.Println("  AWS deploy: Push to ECR and deploy to ECS/App Runner.")
	fmt.Println("  Example:")
	fmt.Println("    aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <account>.dkr.ecr.us-east-1.amazonaws.com")
	fmt.Println("    docker tag " + appName + ":latest <account>.dkr.ecr.us-east-1.amazonaws.com/" + appName + ":latest")
	fmt.Println("    docker push <account>.dkr.ecr.us-east-1.amazonaws.com/" + appName + ":latest")
	fmt.Println("  Then update your ECS task definition or App Runner service.")
	return nil
}
