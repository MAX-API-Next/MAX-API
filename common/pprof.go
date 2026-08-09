package common

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/shirou/gopsutil/cpu"
)

var cpuPercent = cpu.Percent

// Monitor 定时监控cpu使用率，超过阈值输出pprof文件
func Monitor() {
	for {
		captureHighCPUProfile()
		time.Sleep(30 * time.Second)
	}
}

func captureHighCPUProfile() {
	percent, err := cpuPercent(time.Second, false)
	if err != nil {
		SysLog("获取CPU使用率失败 " + err.Error())
		return
	}
	if len(percent) == 0 || percent[0] <= 80 {
		return
	}

	fmt.Println("cpu usage too high")
	if err := os.MkdirAll("./pprof", 0o755); err != nil {
		SysLog("创建pprof文件夹失败 " + err.Error())
		return
	}
	f, err := os.Create("./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405")))
	if err != nil {
		SysLog("创建pprof文件失败 " + err.Error())
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		SysLog("启动pprof失败 " + err.Error())
		return
	}
	time.Sleep(10 * time.Second)
	pprof.StopCPUProfile()
	_ = f.Close()
}
