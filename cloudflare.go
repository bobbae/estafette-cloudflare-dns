package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Cloudflare is the object to perform Cloudflare api calls with
type Cloudflare struct {
	restClient     restClient
	authentication APIAuthentication
	baseURL        string
}

// New returns an initialized APIClient
func New(authentication APIAuthentication) *Cloudflare {

	return &Cloudflare{
		restClient:     new(realRESTClient),
		authentication: authentication,
		baseURL:        "https://api.cloudflare.com/client/v4",
	}
}

func (cf *Cloudflare) getZonesByName(zoneName string) (r zonesResult, err error) {

	// create api url
	findZoneURI := fmt.Sprintf("%v/zones/?name=%v", cf.baseURL, zoneName)

	// fetch result from cloudflare api
	body, err := cf.restClient.Get(findZoneURI, cf.authentication)
	if err != nil {
		return
	}

	json.NewDecoder(bytes.NewReader(body)).Decode(&r)

	if !r.Success {
		err = errors.New("cloudflare: listing zones failed")
		return
	}

	return
}

// GetZoneByDNSName returns the Cloudflare zone by looking it up with a dnsName, possibly including subdomains; also works for TLDs like .co.uk.
func (cf *Cloudflare) GetZoneByDNSName(dnsName string) (r Zone, err error) {

	// split dnsName
	dnsNameParts := strings.Split(dnsName, ".")

	// verify dnsName has enough parts
	if len(dnsNameParts) < 2 {
		err = errors.New("cloudflare: dnsName has too few parts, should at least have a tld and domain name")
		return
	}

	// start taking parts from the end of dnsName and see if cloudflare has a zone for them
	numberOfZoneItems := 2
	zoneNameParts, err := getLastItemsFromSlice(dnsNameParts, numberOfZoneItems)
	if err != nil {
		return
	}

	zoneName := strings.Join(zoneNameParts, ".")
	zonesResult, err := cf.getZonesByName(zoneName)
	if err != nil {
		return
	}

	// if matching zones results fit in a single page get the fully matching zone from the results, otherwise narrow down the search
	if (zonesResult.ResultInfo.Count > 0) && (zonesResult.ResultInfo.Count <= zonesResult.ResultInfo.PerPage) {
		r, err = getMatchingZoneFromZones(zonesResult.Zones, zoneName)
		return
	}

	// if too many zones or none exist for last 2 parts of the dns name, we have to narrow down the search by specifying a more detailed name
	for ((zonesResult.ResultInfo.TotalCount == 0) || (zonesResult.ResultInfo.TotalCount > zonesResult.ResultInfo.PerPage)) && (numberOfZoneItems < len(dnsNameParts)) {
		numberOfZoneItems++
		zoneNameParts, err = getLastItemsFromSlice(dnsNameParts, numberOfZoneItems)
		if err != nil {
			return
		}

		zoneName = strings.Join(zoneNameParts, ".")
		zonesResult, err = cf.getZonesByName(zoneName)
		if err != nil {
			return
		}

		if (zonesResult.ResultInfo.Count > 0) && (zonesResult.ResultInfo.Count <= zonesResult.ResultInfo.PerPage) {
			r, err = getMatchingZoneFromZones(zonesResult.Zones, zoneName)
			return
		}
	}

	err = errors.New("cloudflare: no matching zone has been found")
	return
}

func (cf *Cloudflare) getDNSRecordsByZoneAndName(zone Zone, dnsRecordName string) (r dNSRecordsResult, err error) {

	// create api url
	findDNSRecordURI := fmt.Sprintf("%v/zones/%v/dns_records/?name=%v", cf.baseURL, zone.ID, dnsRecordName)

	// fetch result from cloudflare api
	body, err := cf.restClient.Get(findDNSRecordURI, cf.authentication)
	if err != nil {
		return
	}

	json.NewDecoder(bytes.NewReader(body)).Decode(&r)

	if !r.Success {
		err = errors.New("Listing dns record failed")
		return
	}

	return
}

// GetDNSRecordByDNSName returns the Cloudflare dns record by looking it up with a dnsName.
func (cf *Cloudflare) GetDNSRecordByDNSName(dnsName string) (r DNSRecord, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsName)
	if err != nil {
		return
	}

	// get dns record
	dnsRecordsResult, err := cf.getDNSRecordsByZoneAndName(zone, dnsName)
	if err != nil {
		return
	}

	if dnsRecordsResult.ResultInfo.Count == 0 {
		err = errors.New("No matching dns record has been found")
		return
	}

	r = dnsRecordsResult.DNSRecords[0]

	return
}

func (cf *Cloudflare) createDNSRecordByZone(zone Zone, dnsRecordType, dnsRecordName, dnsRecordContent string) (r createResult, err error) {

	// create record at cloudflare api
	newDNSRecord := DNSRecord{Type: dnsRecordType, Name: dnsRecordName, Content: dnsRecordContent}

	createDNSRecordURI := fmt.Sprintf("%v/zones/%v/dns_records", cf.baseURL, zone.ID)

	body, err := cf.restClient.Post(createDNSRecordURI, newDNSRecord, cf.authentication)
	if err != nil {
		return
	}

	json.NewDecoder(bytes.NewReader(body)).Decode(&r)

	if !r.Success {
		err = errors.New("cloudflare: creating dns record failed")
		return
	}

	return
}

// CreateDNSRecord creates a new dns record.
func (cf *Cloudflare) CreateDNSRecord(dnsRecordType, dnsRecordName, dnsRecordContent string) (r DNSRecord, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsRecordName)
	if err != nil {
		return
	}

	// create record at cloudflare api
	var cloudflareDNSRecordsCreateResult createResult
	cloudflareDNSRecordsCreateResult, err = cf.createDNSRecordByZone(zone, dnsRecordType, dnsRecordName, dnsRecordContent)
	if err != nil {
		return
	}

	r = cloudflareDNSRecordsCreateResult.DNSRecord

	return
}

func (cf *Cloudflare) deleteDNSRecordByDNSRecord(dnsRecord DNSRecord) (r deleteResult, err error) {

	// delete dns record
	deleteDNSRecordURI := fmt.Sprintf("%v/zones/%v/dns_records/%v", cf.baseURL, dnsRecord.ZoneID, dnsRecord.ID)
	body, err := cf.restClient.Delete(deleteDNSRecordURI, cf.authentication)
	if err != nil {
		return
	}

	json.NewDecoder(bytes.NewReader(body)).Decode(&r)

	if !r.Success {
		err = fmt.Errorf("cloudflare: deleting dns record failed | %v | %v", r.Errors, r.Messages)
		return
	}

	return
}

func (cf *Cloudflare) deleteDNSRecordByZone(zone Zone, dnsRecordName string) (r bool, err error) {

	// get dns record
	dnsRecordsResult, err := cf.getDNSRecordsByZoneAndName(zone, dnsRecordName)
	if err != nil {
		return
	}
	if dnsRecordsResult.ResultInfo.Count == 0 {
		err = errors.New("No matching dns record has been found")
		return
	}
	dnsRecord := dnsRecordsResult.DNSRecords[0]

	// delete dns record
	_, err = cf.deleteDNSRecordByDNSRecord(dnsRecord)
	if err != nil {
		return
	}

	r = true

	return
}

// DeleteDNSRecord deletes a dns record.
func (cf *Cloudflare) DeleteDNSRecord(dnsRecordName string) (r bool, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsRecordName)
	if err != nil {
		return
	}

	return cf.deleteDNSRecordByZone(zone, dnsRecordName)
}

func (cf *Cloudflare) updateDNSRecordByDNSRecord(dnsRecord DNSRecord, dnsRecordType, dnsRecordContent string) (r updateResult, err error) {

	// check dnsRecordType
	if dnsRecord.Type != dnsRecordType {
		err = errors.New("Failed updating dns record, you cannot change the type of an existing record")
		return
	}

	dnsRecord.Content = dnsRecordContent

	updateDNSRecordURI := fmt.Sprintf("%v/zones/%v/dns_records/%v", cf.baseURL, dnsRecord.ZoneID, dnsRecord.ID)

	body, err := cf.restClient.Put(updateDNSRecordURI, dnsRecord, cf.authentication)
	if err != nil {
		return
	}

	json.NewDecoder(bytes.NewReader(body)).Decode(&r)

	if !r.Success {
		err = errors.New("cloudflare: update dns record failed")
		return
	}

	return
}

func (cf *Cloudflare) updateDNSRecordByZone(zone Zone, dnsRecordType, dnsRecordName, dnsRecordContent string) (r DNSRecord, err error) {

	// get dns record
	dnsRecordsResult, err := cf.getDNSRecordsByZoneAndName(zone, dnsRecordName)
	if err != nil {
		return
	}
	if dnsRecordsResult.ResultInfo.Count == 0 {
		err = errors.New("No matching dns record has been found")
		return
	}

	r = dnsRecordsResult.DNSRecords[0]

	cloudflareDNSRecordsUpdateResult, err := cf.updateDNSRecordByDNSRecord(r, dnsRecordType, dnsRecordContent)
	if err != nil {
		return
	}

	r = cloudflareDNSRecordsUpdateResult.DNSRecord

	return
}

// UpdateDNSRecord updates an existing dns record.
func (cf *Cloudflare) UpdateDNSRecord(dnsRecordType, dnsRecordName, dnsRecordContent string) (r DNSRecord, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsRecordName)
	if err != nil {
		return
	}

	return cf.updateDNSRecordByZone(zone, dnsRecordType, dnsRecordName, dnsRecordContent)
}

// UpsertDNSRecord either updates or creates a dns record.
func (cf *Cloudflare) UpsertDNSRecord(dnsRecordType, dnsRecordName, dnsRecordContent string) (r DNSRecord, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsRecordName)
	if err != nil {
		return
	}

	// get dns record
	dnsRecordsResult, err := cf.getDNSRecordsByZoneAndName(zone, dnsRecordName)
	if err != nil {
		return
	}

	if dnsRecordsResult.ResultInfo.Count > 1 {
		err = errors.New("Cannot upsert, there's more than 1 record by that name")
		return
	} else if dnsRecordsResult.ResultInfo.Count == 1 {

		r = dnsRecordsResult.DNSRecords[0]

		if dnsRecordType != r.Type {

			// delete record of old type
			_, err = cf.deleteDNSRecordByDNSRecord(r)
			if err != nil {
				return
			}

			// create record of new type
			var cloudflareDNSRecordsCreateResult createResult
			cloudflareDNSRecordsCreateResult, err = cf.createDNSRecordByZone(zone, dnsRecordType, dnsRecordName, dnsRecordContent)
			if err != nil {
				return
			}

			r = cloudflareDNSRecordsCreateResult.DNSRecord

		} else {

			// update record
			var cloudflareDNSRecordsUpdateResult updateResult
			cloudflareDNSRecordsUpdateResult, err = cf.updateDNSRecordByDNSRecord(r, dnsRecordType, dnsRecordContent)
			if err != nil {
				return
			}

			r = cloudflareDNSRecordsUpdateResult.DNSRecord

		}

		return
	}

	// create record
	var cloudflareDNSRecordsCreateResult createResult
	cloudflareDNSRecordsCreateResult, err = cf.createDNSRecordByZone(zone, dnsRecordType, dnsRecordName, dnsRecordContent)
	if err != nil {
		return
	}

	r = cloudflareDNSRecordsCreateResult.DNSRecord

	return
}

// UpdateProxySetting updates the proxied setting for an existing dns record.
func (cf *Cloudflare) UpdateProxySetting(dnsRecordName, proxy string) (r DNSRecord, err error) {

	// get zone
	zone, err := cf.GetZoneByDNSName(dnsRecordName)
	if err != nil {
		return
	}

	// get dns record
	dnsRecordsResult, err := cf.getDNSRecordsByZoneAndName(zone, dnsRecordName)
	if err != nil {
		return
	}

	if dnsRecordsResult.ResultInfo.Count == 0 {
		err = errors.New("No matching dns record has been found")
		return
	} else if dnsRecordsResult.ResultInfo.Count > 1 {
		err = errors.New("Cannot update proxy setting, there's more than 1 record by that name")
		return
	} else if dnsRecordsResult.ResultInfo.Count == 1 {

		r = dnsRecordsResult.DNSRecords[0]

		if r.Proxiable {

			if proxy == "true" {
				r.Proxied = true
			} else {
				r.Proxied = false
			}

			updateDNSRecordURI := fmt.Sprintf("%v/zones/%v/dns_records/%v", cf.baseURL, r.ZoneID, r.ID)

			var body []byte
			body, err = cf.restClient.Put(updateDNSRecordURI, r, cf.authentication)
			if err != nil {
				return
			}

			var ur updateResult

			json.NewDecoder(bytes.NewReader(body)).Decode(&ur)

			if !ur.Success {
				err = errors.New("cloudflare: update dns record failed")
				return
			}
		}
	}

	return
}
