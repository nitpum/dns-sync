package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/cloudflare/cloudflare-go"
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

	api, err := cloudflare.NewWithAPIToken(*argToken)
	if err != nil {
		panic(err)
	}

	onlineDNS := fetchZone(api, *argZoneID)
	var c config
	c.readYAML(*argConfig)
	toInsert, toUpdate, toDelete := c.compareRecord(onlineDNS)
	deleteOnlineRecord(api, *argZoneID, toDelete)
	createOnlineRecord(api, *argZoneID, toInsert)
	updateOnlineRecord(api, *argZoneID, toUpdate)
	// toInsert, toUpdate, toDelete := c.getExists(onlineDNS)
}

func fetchZone(api *cloudflare.API, zoneID string) []cloudflare.DNSRecord {
	log.Info().Msg("Fetch zone")

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
	log.Info().Msgf("Read file: %s\n", filePath)

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
		fmt.Printf("[%s]\t%s \n\t%s \n\tproxy: %t\tpirority: %d\n", r.Type, r.Name, r.Content, r.Proxied, r.Priority)
	}
}

func (c *config) readYAML(filePath string) *config {
	data, err := readFile(filePath)
	if err != nil {
		panic(err)
	}

	log.Info().Msgf("Parse yaml: %s\n", filePath)
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
	log.Info().Msg("Comparing record")

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

	if *flagVerbose {
		fmt.Println("\n\nTo Insert")
		fmt.Println(toInsert)
		fmt.Println("\n\nTo Update")
		fmt.Println(toUpdate)
		fmt.Println("\n\nTo Delete")
		printDNSRecord(toDelete)
	}

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
	log.Info().Msg("Create online records")

	for _, rec := range record {
		_, err := api.CreateDNSRecord(zoneID, cloudflare.DNSRecord{
			Name:    rec.Name,
			Type:    rec.Type,
			Content: rec.Content,
			Proxied: rec.Proxied,
		})

		if err != nil {
			log.Info().Msgf("\tFailed creating record: %s\n", rec.Name)
			log.Logger.Err(err)
		} else {
			log.Info().Msgf("\tCreated record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish creating online records")
}

func updateOnlineRecord(api *cloudflare.API, zoneID string, record []record) {
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
			log.Info().Msgf("\tFailed update record: %s\n", rec.Name)
			log.Logger.Err(err)
		} else {
			log.Info().Msgf("\tUpdated record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish creating online records")
}

func deleteOnlineRecord(api *cloudflare.API, zoneID string, record []cloudflare.DNSRecord) {
	log.Info().Msg("Delete online records")

	for _, rec := range record {
		err := api.DeleteDNSRecord(zoneID, rec.ID)

		if err != nil {
			log.Info().Msgf("\tRecord: %s\n", rec.Name)
			log.Logger.Err(err)
		} else {
			log.Info().Msgf("\tDeleted record: [%s] %s\n", rec.Type, rec.Name)
		}
	}

	log.Info().Msg("Finish deleting online records")
}
