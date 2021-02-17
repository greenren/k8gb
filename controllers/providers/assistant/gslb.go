package assistant

import (
	"context"
	coreerrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/AbsaOSS/k8gb/controllers/internal/utils"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const coreDNSExtServiceName = "k8gb-coredns-lb"

// GslbLoggerAssistant is common wrapper operating on GSLB instance
// it directly logs messages into logr.Logger and use apimachinery client
// to call kubernetes API
type GslbLoggerAssistant struct {
	log           logr.Logger
	client        client.Client
	k8gbNamespace string
	edgeDNSServer string
}

func NewGslbAssistant(client client.Client, log logr.Logger, k8gbNamespace, edgeDNSServer string) *GslbLoggerAssistant {
	return &GslbLoggerAssistant{
		client:        client,
		log:           log,
		k8gbNamespace: k8gbNamespace,
		edgeDNSServer: edgeDNSServer,
	}
}

// CoreDNSExposedIPs retrieves list of IP's exposed by CoreDNS
func (r *GslbLoggerAssistant) CoreDNSExposedIPs() ([]string, error) {
	coreDNSService := &corev1.Service{}
	err := r.client.Get(context.TODO(),
		types.NamespacedName{Namespace: r.k8gbNamespace, Name: coreDNSExtServiceName}, coreDNSService)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Info("Can't find %s service", coreDNSExtServiceName)
		}
		return nil, err
	}
	var lbHostname string
	if len(coreDNSService.Status.LoadBalancer.Ingress) > 0 {
		lbHostname = coreDNSService.Status.LoadBalancer.Ingress[0].Hostname
	} else {
		errMessage := fmt.Sprintf("no Ingress LoadBalancer entries found for %s serice", coreDNSExtServiceName)
		r.Info(errMessage)
		err := coreerrors.New(errMessage)
		return nil, err
	}
	IPs, err := utils.Dig(r.edgeDNSServer, lbHostname)
	if err != nil {
		r.Info("Can't dig k8gb-coredns-lb service loadbalancer fqdn %s (%s)", lbHostname, err)
		return nil, err
	}
	return IPs, nil
}

// GslbIngressExposedIPs retrieves list of IP's exposed by all GSLB ingresses
func (r *GslbLoggerAssistant) GslbIngressExposedIPs(gslb *k8gbv1beta1.Gslb) ([]string, error) {
	nn := types.NamespacedName{
		Name:      gslb.Name,
		Namespace: gslb.Namespace,
	}

	gslbIngress := &v1beta1.Ingress{}

	err := r.client.Get(context.TODO(), nn, gslbIngress)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Info("Can't find gslb Ingress: %s", gslb.Name)
		}
		return nil, err
	}

	var gslbIngressIPs []string

	for _, ip := range gslbIngress.Status.LoadBalancer.Ingress {
		if len(ip.IP) > 0 {
			gslbIngressIPs = append(gslbIngressIPs, ip.IP)
		}
		if len(ip.Hostname) > 0 {
			IPs, err := utils.Dig(r.edgeDNSServer, ip.Hostname)
			if err != nil {
				r.Info("Dig error: %s", err)
				return nil, err
			}
			gslbIngressIPs = append(gslbIngressIPs, IPs...)
		}
	}

	return gslbIngressIPs, nil
}

// SaveDNSEndpoint update DNS endpoint or create new one if doesnt exist
func (r *GslbLoggerAssistant) SaveDNSEndpoint(namespace string, i *externaldns.DNSEndpoint) error {
	found := &externaldns.DNSEndpoint{}
	err := r.client.Get(context.TODO(), types.NamespacedName{
		Name:      i.Name,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		// Create the DNSEndpoint
		r.Info("Creating a new DNSEndpoint:\n %s", utils.ToString(i))
		err = r.client.Create(context.TODO(), i)

		if err != nil {
			// Creation failed
			r.Error(err, "Failed to create new DNSEndpoint DNSEndpoint.Namespace: %s DNSEndpoint.Name %s",
				i.Namespace, i.Name)
			return err
		}
		// Creation was successful
		return nil
	} else if err != nil {
		// Error that isn't due to the service not existing
		r.Error(err, "Failed to get DNSEndpoint")
		return err
	}

	// Update existing object with new spec
	found.Spec = i.Spec
	err = r.client.Update(context.TODO(), found)

	if err != nil {
		// Update failed
		r.Error(err, "Failed to update DNSEndpoint DNSEndpoint.Namespace %s DNSEndpoint.Name %s",
			found.Namespace, found.Name)
		return err
	}
	return nil
}

// RemoveEndpoint removes endpoint
func (r *GslbLoggerAssistant) RemoveEndpoint(endpointName string) error {
	r.Info("Removing endpoint %s.%s", r.k8gbNamespace, endpointName)
	dnsEndpoint := &externaldns.DNSEndpoint{}
	err := r.client.Get(context.Background(), client.ObjectKey{Namespace: r.k8gbNamespace, Name: endpointName}, dnsEndpoint)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Info("%s", err)
			return nil
		}
		return err
	}
	err = r.client.Delete(context.TODO(), dnsEndpoint)
	return err
}

// InspectTXTThreshold inspects fqdn TXT record from edgeDNSServer. If record doesn't exists or timestamp is greater than
// splitBrainThreshold the error is returned. In case fakeDNSEnabled is true, 127.0.0.1:7753 is used as edgeDNSServer
func (r *GslbLoggerAssistant) InspectTXTThreshold(fqdn string, fakeDNSEnabled bool, splitBrainThreshold time.Duration) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(fqdn), dns.TypeTXT)
	ns := overrideWithFakeDNS(fakeDNSEnabled, r.edgeDNSServer)
	txt, err := dns.Exchange(m, ns)
	if err != nil {
		r.Info("Error contacting EdgeDNS server (%s) for TXT split brain record: (%s)", ns, err)
		return err
	}
	var timestamp string
	if len(txt.Answer) > 0 {
		if t, ok := txt.Answer[0].(*dns.TXT); ok {
			r.Info("Split brain TXT raw record: %s", t.String())
			timestamp = strings.Split(t.String(), "\t")[4]
			timestamp = strings.Trim(timestamp, "\"") // Otherwise time.Parse() will miserably fail
		}
	}

	if len(timestamp) > 0 {
		r.Info("Split brain TXT raw time stamp: %s", timestamp)
		timeFromTXT, err := time.Parse("2006-01-02T15:04:05", timestamp)
		if err != nil {
			return err
		}

		r.Info("Split brain TXT parsed time stamp: %s", timeFromTXT)
		now := time.Now().UTC()

		diff := now.Sub(timeFromTXT)
		r.Info("Split brain TXT time diff: %s", diff)

		if diff > splitBrainThreshold {
			return errors.NewResourceExpired(fmt.Sprintf("Split brain TXT record expired the time threshold: (%s)", splitBrainThreshold))
		}
		return nil
	}
	return errors.NewResourceExpired(fmt.Sprintf("Can't find split brain TXT record at EdgeDNS server(%s) and record %s ", ns, fqdn))
}

func (r *GslbLoggerAssistant) GetExternalTargets(host string, fakeDNSEnabled bool, extGslbClusters []string) (targets []string) {
	targets = []string{}
	for _, cluster := range extGslbClusters {
		r.Info("Adding external Gslb targets from %s cluster...", cluster)
		g := new(dns.Msg)
		host = fmt.Sprintf("localtargets-%s.", host) // Convert to true FQDN with dot at the end. Otherwise dns lib freaks out
		g.SetQuestion(host, dns.TypeA)

		ns := overrideWithFakeDNS(fakeDNSEnabled, cluster)

		a, err := dns.Exchange(g, ns)
		if err != nil {
			r.Info("Error contacting external Gslb cluster(%s) : (%v)", cluster, err)
			return
		}
		var clusterTargets []string

		for _, A := range a.Answer {
			IP := strings.Split(A.String(), "\t")[4]
			clusterTargets = append(clusterTargets, IP)
		}
		if len(clusterTargets) > 0 {
			targets = append(targets, clusterTargets...)
			r.Info("Added external %s Gslb targets from %s cluster", clusterTargets, cluster)
		}
	}
	return
}

// Info wraps private logger and provides log.Info()
func (r *GslbLoggerAssistant) Info(msg string, args ...interface{}) {
	r.log.Info(fmt.Sprintf(msg, args...))
}

// Error wraps private logger and provides log.Error()
func (r *GslbLoggerAssistant) Error(err error, msg string, args ...interface{}) {
	r.log.Error(err, fmt.Sprintf(msg, args...))
}

func overrideWithFakeDNS(fakeDNSEnabled bool, server string) (ns string) {
	if fakeDNSEnabled {
		ns = "127.0.0.1:7753"
	} else {
		ns = fmt.Sprintf("%s:53", server)
	}
	return
}
