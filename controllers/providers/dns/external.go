package dns

import (
	"fmt"
	"sort"
	"strings"

	assistant2 "github.com/AbsaOSS/k8gb/controllers/providers/assistant"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

type ExternalDNSType string

const (
	externalDNSTypeNS1     ExternalDNSType = "ns1"
	externalDNSTypeRoute53 ExternalDNSType = "route53"
)

type ExternalDNSProvider struct {
	assistant    assistant2.IAssistant
	dnsType      ExternalDNSType
	config       depresolver.Config
	endpointName string
}

func NewExternalDNS(dnsType ExternalDNSType, config depresolver.Config, assistant assistant2.IAssistant) *ExternalDNSProvider {
	return &ExternalDNSProvider{
		assistant:    assistant,
		dnsType:      dnsType,
		config:       config,
		endpointName: fmt.Sprintf("k8gb-ns-%s", dnsType),
	}
}

func (p *ExternalDNSProvider) CreateZoneDelegationForExternalDNS(gslb *k8gbv1beta1.Gslb) error {
	ttl := externaldns.TTL(gslb.Spec.Strategy.DNSTtlSeconds)
	p.assistant.Info("Creating/Updating DNSEndpoint CRDs for %s...", p)
	var NSServerList []string
	NSServerList = append(NSServerList, nsServerName(p.config))
	NSServerList = append(NSServerList, nsServerNameExt(p.config)...)
	sort.Strings(NSServerList)
	var NSServerIPs []string
	var err error
	if p.config.CoreDNSExposed {
		NSServerIPs, err = p.assistant.CoreDNSExposedIPs()
	} else {
		NSServerIPs, err = p.assistant.GslbIngressExposedIPs(gslb)
	}
	if err != nil {
		return err
	}
	NSRecord := &externaldns.DNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:        p.endpointName,
			Namespace:   p.config.K8gbNamespace,
			Annotations: map[string]string{"k8gb.absa.oss/dnstype": string(p.dnsType)},
		},
		Spec: externaldns.DNSEndpointSpec{
			Endpoints: []*externaldns.Endpoint{
				{
					DNSName:    p.config.DNSZone,
					RecordTTL:  ttl,
					RecordType: "NS",
					Targets:    NSServerList,
				},
				{
					DNSName:    nsServerName(p.config),
					RecordTTL:  ttl,
					RecordType: "A",
					Targets:    NSServerIPs,
				},
			},
		},
	}
	err = p.assistant.SaveDNSEndpoint(p.config.K8gbNamespace, NSRecord)
	return err
}

func (p *ExternalDNSProvider) Finalize(*k8gbv1beta1.Gslb) error {
	return p.assistant.RemoveEndpoint(p.endpointName)
}

func (p *ExternalDNSProvider) GetExternalTargets(host string) (targets []string) {
	return p.assistant.GetExternalTargets(host, p.config.Override.FakeDNSEnabled, nsServerNameExt(p.config))
}

func (p *ExternalDNSProvider) GslbIngressExposedIPs(gslb *k8gbv1beta1.Gslb) ([]string, error) {
	return p.assistant.GslbIngressExposedIPs(gslb)
}

func (p *ExternalDNSProvider) SaveDNSEndpoint(gslb *k8gbv1beta1.Gslb, i *externaldns.DNSEndpoint) error {
	return p.assistant.SaveDNSEndpoint(gslb.Namespace, i)
}

func (p *ExternalDNSProvider) String() string {
	return strings.ToUpper(string(p.dnsType))
}
