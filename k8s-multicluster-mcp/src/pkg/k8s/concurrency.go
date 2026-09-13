package k8s

import "sync"

const (
	defaultKubernetesReadConcurrency = 8
	defaultKubernetesLogConcurrency  = 4
)

// runBoundedTasks runs independent work concurrently while limiting the number
// of tasks that can execute at once. Each task owns its output slot; callers
// assemble shared response maps after all tasks complete.
func runBoundedTasks(limit int, tasks []func()) {
	if len(tasks) == 0 {
		return
	}
	if limit <= 0 || limit > len(tasks) {
		limit = len(tasks)
	}

	semaphore := make(chan struct{}, limit)
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			task()
		}()
	}
	wg.Wait()
}

func runConcurrentTasks(tasks ...func()) {
	runBoundedTasks(defaultKubernetesReadConcurrency, tasks)
}
