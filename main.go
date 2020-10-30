package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tidwall/gjson"
	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v3"
)

var (
	argToken    = kingpin.Arg("token", "Access token used in API").Required().String()
	argZoneID   = kingpin.Arg("zone", "Zone ID").Required().String()
	argConfig   = kingpin.Arg("config", "Config file").Required().String()
	flagVerbose = kingpin.Flag("verbose", "Verbose").Bool()
)

type record struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	Content      string `yaml:"content"`
	Proxied      bool   `yaml:"proxy"`
	OnlineRecord cloudflare.DNSRecord
}

type config struct {
	Domain  string   `yaml:"domain"`
	Records []record `yaml:"records"`
}

func main() {
	kingpin.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	log.Info().Bool("VERBOSE", *flagVerbose)
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *flagVerbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	api, err := cloudflare.NewWithAPIToken(*argToken)
	if err != nil {
		panic(err)
	}

	onlineDNS := fetchZone(api, *argZoneID)
	var c config
	c.readYAML(*argConfig)

	toInsert, toUpdate, toDelete := c.compareRecord(onlineDNS)
	deleteOnlineRecord(api, *argZoneID, toDelete)
	updateOnlineRecord(api, *argZoneID, toUpdate)
	createOnlineRecord(api, *argZoneID, toInsert)
}

func fetchZone(api *cloudflare.API, zoneID string) []cloudflare.DNSRecord {
	log.Info().Msg("Fetching zone")

	recs, err := api.DNSRecords(zoneID, cloudflare.DNSRecord{})
	if err != nil {
		panic(err)
	}

	if !*flagVerbose {
		return recs
	}

	printDNSRecord(recs)

	return recs
}

func readConfig(json string) {
	log.Info().Msg("Read config")
	result := gjson.Get(string(json), "subdomains")
	println(result.String())
}

func readJSON(path string) ([]byte, error) {
	log.Info().Msgf("Read json file: %s\n", path)

	jsonFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	return byteValue, nil
}

func readFile(filePath string) ([]byte, error) {
	log.Info().Msgf("Read file: %s", filePath)

	file, err := os.Open(filePath)
	if err != nil {
		log.Err(err)
		return nil, err
	}

	defer file.Close()
	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		log.Err(err)
		return nil, err
	}

	return byteValue, nil
}

func printDNSRecord(onlineRec []cloudflare.DNSRecord) {
	for _, r := range onlineRec {
		if r.Type != "A" && r.Type != "CNAME" {
			continue
		}

		log.Debug().Bool("Proxied", r.Proxied).Str("Content", r.Content).Str("Name", r.Name).Str("Type", r.Type).Msg("")
	}
}

func printRecords(rec []record) {
	for _, r := range rec {
		if r.Type != "A" && r.Type != "CNAME" {
			continue
		}
		log.Debug().Str("Name", r.Name).Str("Type", r.Type).Str("Content", r.Content).Bool("Proxied", r.Proxied).Msg("")
	}
}

func (c *config) readYAML(filePath string) *config {
	data, err := readFile(filePath)
	if err != nil {
		panic(err)
	}

	log.Info().Msgf("Parse yaml: %s", filePath)
	if err := yaml.Unmarshal(data, c); err != nil {
		panic(err)
	}

	return c
}

func (r *record) isMatchRecord(domain string, rec cloudflare.DNSRecord) bool {
	if r.toFullname(domain) == rec.Name && r.Type == rec.Type {
		return true
	}

	return false
}

func (r *record) isNeedUpdate() bool {
	if r.Proxied != r.OnlineRecord.Proxied || r.Content != r.OnlineRecord.Content {
		return true
	}

	return false
}

func (r *record) isMatchConfigRecord(domain string, rec record) bool {
	if r.toFullname(domain) == rec.Name && r.Type == rec.Type {
		return true
	}

	return false
}

func (c *config) compareRecord(onlineRec []cloudflare.DNSRecord) ([]record, []record, []cloudflare.DNSRecord) {
	log.Info().Msg("Classify records")

	toUpdate := []record{}
	toDelete := []cloudflare.DNSRecord{}
	for _, rec := range onlineRec {
		if rec.Type != "A" && rec.Type != "CNAME" {
			continue
		}

		added := false
		for _, recCfg := range c.Records {
			if recCfg.isMatchRecord(c.Domain, rec) && !added {
				added = true
				recCfg.OnlineRecord = rec
				toUpdate = append(toUpdate, recCfg)
				break
			}
		}

		if !added {
			toDelete = append(toDelete, rec)
		}
	}

	toInsert := []record{}
	for _, recCfg := range c.Records {
		isUpdate := false
		for _, recInUpdate := range toUpdate {
			if recCfg.Name == recInUpdate.Name {
				isUpdate = true
				break
			}
		}
		if !isUpdate {
			toInsert = append(toInsert, recCfg)
		}
	}

	needToUpdate := []record{}
	for _, rec := range toUpdate {
		if !rec.isNeedUpdate() {
			continue
		}

		needToUpdate = append(needToUpdate, rec)
	}

	toUpdate = needToUpdate

	log.Info().Msgf("To create (%d)", len(toInsert))
	printRecords(toInsert)
	log.Info().Msgf("To update (%d)", len(toInsert))
	printRecords(toUpdate)
	log.Info().Msgf("To delete (%d)", len(toInsert))
	printDNSRecord(toDelete)

	return toInsert, toUpdate, toDelete
}

func (r *record) toFullname(domain string) string {
	switch r.Name {
	case "@":
		return domain
	}

	return fmt.Sprintf("%s.%s", r.Name, domain)
}

func (c *config) recordRemoveAt(i int) {
	if i == 0 {
		c.Records = []record{}
		return
	}

	c.Records[i] = c.Records[len(c.Records)-1]

	c.Records = c.Records[:len(c.Records)-1]
}

func createOnlineRecord(api *cloudflare.API, zoneID string, record []record) {
	if len(record) <= 0 {
		return
	}

	log.Info().Msg("Create online records")

	for _, rec := range record {
		_, err := api.CreateDNSRecord(zoneID, cloudflare.DNSRecord{
			Name:    rec.Name,
			Type:    rec.Type,
			Content: rec.Content,
			Proxied: rec.Proxied,
		})

		if err != nil {
			log.Error().Msgf("Failed creating record: %s\n", rec.Name)
			log.Error().Err(err)
		} else {
			log.Info().Msgf("Created record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish creating online records")
}

func updateOnlineRecord(api *cloudflare.API, zoneID string, record []record) {
	if len(record) <= 0 {
		return
	}

	log.Info().Msg("Update online records")

	for _, rec := range record {

		if !rec.isNeedUpdate() {
			continue
		}

		err := api.UpdateDNSRecord(zoneID, rec.OnlineRecord.ID, cloudflare.DNSRecord{
			Name:    rec.Name,
			Type:    rec.Type,
			Content: rec.Content,
			Proxied: rec.Proxied,
		})

		if err != nil {
			log.Error().Msgf("Failed update record: %s\n", rec.Name)
			log.Error().Err(err)
		} else {
			log.Info().Msgf("Updated record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish creating online records")
}

func deleteOnlineRecord(api *cloudflare.API, zoneID string, record []cloudflare.DNSRecord) {
	if len(record) <= 0 {
		return
	}

	log.Info().Msg("Delete online records")

	for _, rec := range record {
		err := api.DeleteDNSRecord(zoneID, rec.ID)

		if err != nil {
			log.Error().Msgf("Record: %s\n", rec.Name)
			log.Error().Err(err)
		} else {
			log.Info().Msgf("Deleted record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish deleting online records")
}
