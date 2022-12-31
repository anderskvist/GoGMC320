package main

/*
 * Documentation
 * http://www.gqelectronicsllc.com/download/GQ-RFC1201.txt
 *
 * Examples
 * https://github.com/chaim-zax/gq-gmc-control/blob/master/gq_gmc.py
 */

import (
	"encoding/binary"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
	"github.com/tarm/serial"
	"gopkg.in/ini.v1"

	"github.com/anderskvist/GoHelpers/log"
	"github.com/anderskvist/GoHelpers/version"
)

var s *serial.Port
var cfg *ini.File
var err error
var cfg2 gmccfg
var acpm _acpm

type gmccfg struct {
	calibrate1_cpm uint16
	calibrate1_sv  float32
	calibrate2_cpm uint16
	calibrate2_sv  float32
	calibrate3_cpm uint16
	calibrate3_sv  float32
}

type _acpm struct {
	count uint32
	total uint64
}

func sendCommand(command string) []byte {
	s.Flush()
	n, err := s.Write([]byte(command))

	if err != nil {
		log.Fatal(err)
	}
	log.Info(command)

	if command == "<GETCFG>>" {
		// Allow time for longer responses - GETCFG for example
		time.Sleep(100 * 1000 * 1000)
	}

	buf := make([]byte, 4096)

	n, err = s.Read(buf)
	if err != nil {
		log.Fatal(err)
	}
	s.Flush()
	log.Debugf("%x", buf[:n])
	return buf[:n]
}

func getVer() string {
	buf := sendCommand("<GETVER>>")
	return string(buf)
}

func getDateTime() time.Time {
	buf := sendCommand("<GETDATETIME>>")

	year := buf[0]
	month := buf[1]
	day := buf[2]
	hour := buf[3]
	minute := buf[4]
	second := buf[5]

	t := time.Date(2000+int(year), time.Month(int(month)), int(day), int(hour), int(minute), int(second), 0, time.UTC)
	log.Infof("%s\n", t.UTC())

	return t
}

func getCpm() uint16 {
	buf := sendCommand("<GETCPM>>")
	val := binary.BigEndian.Uint16(buf)
	log.Infof("%d CPM", val)

	acpm.count++
	acpm.total += uint64(val)

	return val
}

func calcSv(cfg gmccfg, cpm uint16) float32 {
	log.Info("Calculating μSv (micro sievert)")
	cal1_sv := cfg.calibrate1_sv * 1000 / float32(cfg.calibrate1_cpm)
	cal2_sv := cfg.calibrate2_sv * 1000 / float32(cfg.calibrate2_cpm)
	cal3_sv := cfg.calibrate3_sv * 1000 / float32(cfg.calibrate3_cpm)
	cal_sv := (cal1_sv + cal2_sv + cal3_sv) / 3

	val := cal_sv * float32(cpm) / 1000
	log.Infof("%0.2f μSv\n", val)

	return val
}

func calcAcpm() float32 {
	log.Info("Calculating Average CPM")
	val := float32(acpm.total) / float32(acpm.count)
	log.Infof("%0.2f ACPM", val)

	return val
}

func getVolt() float32 {
	buf := sendCommand("<GETVOLT>>")
	val := float32(buf[0]) / 10
	log.Infof("%f", val)
	return val
}

func getCfg() gmccfg {
	buf := sendCommand("<GETCFG>>")
	if len(buf) != 256 {
		os.Exit(0)
	}

	cfg := gmccfg{}
	cfg.calibrate1_cpm = binary.BigEndian.Uint16(buf[0x08 : 0x08+2])
	cfg.calibrate1_sv = math.Float32frombits(binary.LittleEndian.Uint32(buf[0x0a : 0x0a+4]))

	cfg.calibrate2_cpm = binary.BigEndian.Uint16(buf[0x0e : 0x0e+2])
	cfg.calibrate2_sv = math.Float32frombits(binary.LittleEndian.Uint32(buf[0x10 : 0x10+4]))

	cfg.calibrate3_cpm = binary.BigEndian.Uint16(buf[0x14 : 0x14+2])
	cfg.calibrate3_sv = math.Float32frombits(binary.LittleEndian.Uint32(buf[0x16 : 0x16+4]))

	return cfg
}

func initCommunication() {
	c := &serial.Config{Name: "/dev/ttyUSB0", Baud: 115200}
	s, err = serial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
	}
	//getVer()
	//getVolt()
	//getDateTime()
	cfg2 = getCfg()
}

func submitDataRadmonOrg(cpm uint16) {
	user := cfg.Section("radmon.org").Key("user").MustString("")
	password := cfg.Section("radmon.org").Key("password").MustString("")

	log.Debug("Sending data to radmon.org")
	req, err := http.NewRequest("GET", "https://radmon.org/radmon.php?function=submit&user="+user+"&password="+password+"&value="+strconv.FormatInt(int64(cpm), 10)+"&unit=CPM", nil)

	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error reading response. ", err)
	}
	defer resp.Body.Close()
	log.Debug("HTTP Status: ", resp.StatusCode)
}

func SaveToInflux(cpm uint16, usv float32, acpm float32) {

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     cfg.Section("influxdb").Key("url").String(),
		Username: cfg.Section("influxdb").Key("username").String(),
		Password: cfg.Section("influxdb").Key("password").String(),
	})

	log.Debug("Sending data to influxdb")

	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  cfg.Section("influxdb").Key("database").String(),
		Precision: "s",
	})

	//tags := map[string]string{"fabnr": strconv.Itoa(dviData.Fabnr)}

	data := map[string]interface{}{
		"CPM":  cpm,
		"USV":  usv,
		"ACPM": acpm,
	}

	points, err := client.NewPoint(
		"data",
		nil,
		data,
		time.Now(),
	)
	bp.AddPoint(points)

	// Write the batch
	if err := c.Write(bp); err != nil {
		log.Fatal(err)
	}

	// Close client resources
	if err := c.Close(); err != nil {
		log.Fatal(err)
	}
}

func main() {

	cfg, err = ini.Load(os.Args[1])

	if err != nil {
		log.Criticalf("Fail to read file: %v", err)
		os.Exit(1)
	}

	log.Infof("GoGMC320 version: %s.\n", version.Version)

	initCommunication()

	poll := cfg.Section("main").Key("poll").MustInt(60)
	log.Infof("Polltime is %d seconds.\n", poll)

	ticker := time.NewTicker(time.Duration(poll) * time.Second)
	for ; true; <-ticker.C {
		log.Notice("Tick")
		cpm := getCpm()
		usv := calcSv(cfg2, cpm)
		acpm := calcAcpm()
		submitDataRadmonOrg(cpm)
		SaveToInflux(cpm, usv, acpm)
	}
}
