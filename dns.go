package cloudxino

import (
	"context"
	"errors"
	"net/http"
)

// DNSService manages hosted DNS zones and their records (/v1/dns/zones).
type DNSService struct{ c *Client }

// ListZones lists the team's zones (without their records).
func (s *DNSService) ListZones(ctx context.Context) ([]DnsZone, error) {
	return getList[DnsZone](ctx, s.c, "/v1/dns/zones", nil)
}

// GetZone returns a zone with its records.
func (s *DNSService) GetZone(ctx context.Context, id string) (*DnsZone, error) {
	return getOne[DnsZone](ctx, s.c, "/v1/dns/zones"+p(id))
}

func (s *DNSService) CreateZone(ctx context.Context, name string, idempotencyKey ...string) (*DnsZone, error) {
	return send[DnsZone](ctx, s.c, http.MethodPost, "/v1/dns/zones", ZoneIn{Name: name}, idempotencyKey)
}

func (s *DNSService) DeleteZone(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/dns/zones"+p(id))
}

func (s *DNSService) GetRecord(ctx context.Context, zoneID, recordID string) (*DnsRecord, error) {
	return getOne[DnsRecord](ctx, s.c, "/v1/dns/zones"+p(zoneID, "records", recordID))
}

// CreateRecord adds a record and returns it.
func (s *DNSService) CreateRecord(ctx context.Context, zoneID string, req RecordIn, idempotencyKey ...string) (*DnsRecord, error) {
	zone, err := send[DnsZone](ctx, s.c, http.MethodPost, "/v1/dns/zones"+p(zoneID, "records"), req, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if zone.Record == nil {
		return nil, errors.New("cloudxino: the API did not return the new record (server older than 2026-11-15?)")
	}
	return zone.Record, nil
}

// UpdateRecord replaces a record's name, type, content, ttl and priority, and returns it.
func (s *DNSService) UpdateRecord(ctx context.Context, zoneID, recordID string, req RecordIn) (*DnsRecord, error) {
	zone, err := send[DnsZone](ctx, s.c, http.MethodPatch, "/v1/dns/zones"+p(zoneID, "records", recordID), req, nil)
	if err != nil {
		return nil, err
	}
	if zone.Record == nil {
		return nil, errors.New("cloudxino: the API did not return the record")
	}
	return zone.Record, nil
}

func (s *DNSService) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return del(ctx, s.c, "/v1/dns/zones"+p(zoneID, "records", recordID))
}

// Export returns the zone as a BIND zone file.
func (s *DNSService) Export(ctx context.Context, zoneID string) (string, error) {
	var raw []byte
	err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/dns/zones" + p(zoneID, "export"), raw: &raw}, nil)
	return string(raw), err
}

// Import adds the records of a BIND zone file.
func (s *DNSService) Import(ctx context.Context, zoneID, zoneFile string) error {
	return s.c.do(ctx, request{method: http.MethodPost, path: "/v1/dns/zones" + p(zoneID, "import"), body: ImportIn{Text: zoneFile}}, nil)
}
