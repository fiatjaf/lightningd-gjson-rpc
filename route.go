package lightning

import (
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
)

var (
	lastSynced = time.Now().AddDate(0, 0, -1)

	g *Graph
)

type Graph struct {
	client *Client

	idx        int64
	nodeIndex  map[string]int64
	nodeMapIdx map[int64]*Node
	nodeMapId  map[string]*Node
	channelMap map[string]map[string]*Channel

	msatoshi int64
}

func (g *Graph) From(idx int64) graph.Nodes {
	node := g.nodeMapIdx[idx]

	from := g.channelMap[node.Id]

	nodes := Nodes{
		pos:  0,
		list: make([]*Node, len(from)),
	}

	i := 0
	for _, target := range from {
		nodes.list[i] = g.nodeMapId[target.Destination]
		i++
	}

	return nodes
}

func (g *Graph) Edge(x, y int64) graph.Edge {
	if channel, ok := g.channelMap[g.nodeMapIdx[x].Id][g.nodeMapIdx[y].Id]; ok {
		// return false if maxhtlc/minhtlc doesn't fit
		// TODO
		return channel
	}
	return nil
}

func (g *Graph) Weighted(x, y int64) (w float64, ok bool) {
	if x == y {
		return 0, true
	}

	if ichannel := g.Edge(x, y); ichannel != nil {
		channel := ichannel.(*Channel)
		// apply riskfactor
		// TODO

		return channel.Fee(g.msatoshi), true
	}

	return 0, false
}

func (g *Graph) Sync() error {
	// reset our data
	g.nodeIndex = make(map[string]int64)
	g.nodeMapId = make(map[string]*Node)
	g.nodeMapIdx = make(map[int64]*Node)
	g.channelMap = make(map[string]map[string]*Channel)
	g.idx = 0

	// get data from normal LN gossip
	res, err := g.client.Call("listchannels")
	if err != nil {
		return err
	}

	for _, ch := range res.Get("channels").Array() {
		htlcmin, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_minimum_msat").String(), "m")[0], 10, 64)
		htlcmax, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_maximum_msat").String(), "m")[0], 10, 64)

		channel := Channel{
			g:                   g,
			Source:              ch.Get("source").String(),
			Destination:         ch.Get("destination").String(),
			ShortChannelID:      ch.Get("short_channel_id").String(),
			Satoshis:            ch.Get("satoshis").Int(),
			BaseFeeMillisatoshi: ch.Get("base_fee_millisatoshi").Int(),
			FeePerMillionth:     ch.Get("fee_per_millionth").Int(),
			Delay:               ch.Get("delay").Int(),
			HtlcMinimumMsat:     htlcmin,
			HtlcMaximumMsat:     htlcmax,
		}

		if from, ok := g.channelMap[channel.Source]; !ok {
			from := map[string]*Channel{
				channel.Destination: &channel,
			}
			g.channelMap[channel.Source] = from
		} else {
			from[channel.Destination] = &channel
			g.channelMap[channel.Source] = from
		}

		if _, ok := g.channelMap[channel.Destination]; !ok {
			g.channelMap[channel.Destination] = make(map[string]*Channel)
		}

		node := Node{g, channel.Source, ""}
		g.nodeMapId[channel.Source] = &node
		g.nodeMapIdx[g.idx] = &node
		g.nodeIndex[channel.Source] = g.idx
		g.idx++

		node = Node{g, channel.Destination, ""}
		g.nodeMapId[channel.Destination] = &node
		g.nodeMapIdx[g.idx] = &node
		g.nodeIndex[channel.Destination] = g.idx
		g.idx++
	}

	// get data from our custom servers
	// TODO

	// reset counter
	lastSynced = time.Now()

	return nil
}

type Node struct {
	g *Graph

	Id       string `json:"id"`
	Features string `json:"features"`
}

func (n *Node) ID() int64 { return n.g.nodeIndex[n.Id] }

type Nodes struct {
	g *Graph

	pos  int
	list []*Node
}

func (ns Nodes) Next() bool {
	if len(ns.list) > ns.pos+1 {
		ns.pos++
		return true
	}
	return false
}
func (ns Nodes) Len() int {
	return len(ns.list)
}
func (ns Nodes) Reset() {
	ns.pos = 0
}
func (ns Nodes) Node() graph.Node {
	return ns.list[ns.pos]
}

type Channel struct {
	g *Graph

	Source              string `json:"source"`
	Destination         string `json:"destination"`
	ShortChannelID      string `json:"short_channel_id"`
	Satoshis            int64  `json:"satoshis"`
	BaseFeeMillisatoshi int64  `json:"base_fee_millisatoshi"`
	FeePerMillionth     int64  `json:"fee_per_millionth"`
	Delay               int64  `json:"delay"`
	HtlcMinimumMsat     int64  `json:"htlc_minimum_msat"`
	HtlcMaximumMsat     int64  `json:"htlc_maximum_msat"`
}

func (c *Channel) From() graph.Node { return c.g.nodeMapId[c.Source] }
func (c *Channel) To() graph.Node   { return c.g.nodeMapId[c.Destination] }
func (c *Channel) ReversedEdge() graph.Edge {
	ch, ok := c.g.channelMap[c.Destination][c.Source]
	if !ok {
		return nil
	}
	return ch
}

func (c *Channel) Fee(msatoshi int64) float64 {
	return float64(c.BaseFeeMillisatoshi) + float64(c.FeePerMillionth*msatoshi)/1000000
}

type RouteHop struct {
	Id         string `json:"id"`
	Msatoshi   int64  `json:"msatoshi"`
	AmountMsat string `json:"amount_msat,omitempty"`
	Delay      int64  `json:"delay"`
	Style      string `json:"style,omitempty"`
}

func (ln *Client) GetRoute(
	id string,
	msatoshi int64,
	riskfactor int,
	cltv int64,
	fromid string,
	fuzzpercent float64,
	exclude []string,
) (route []RouteHop, err error) {
	// fail obvious errors
	if id == fromid {
		return nil, errors.New("start == end")
	}

	// init graph
	if g == nil {
		g = &Graph{client: ln}
	}

	// sync graph
	if lastSynced.Before(time.Now().Add(-(time.Minute * 30))) {
		if err = g.Sync(); err != nil {
			return
		}
	}

	// exclude channels
	// TODO
	// (set maxhtlc to zero in these)

	// get the best path
	start, ok := g.nodeMapId[fromid]
	if !ok {
		return nil, errors.New("start node not known")
	}

	end, ok := g.nodeIndex[id]
	if !ok {
		return nil, errors.New("end node not known")
	}

	shortest := path.DijkstraFrom(start, g)
	path, _ := shortest.To(end)

	if path == nil {
		return nil, errors.New("no path found")
	}

	// calculate fuzz
	if fuzzpercent > 0 {
		msatoshi = msatoshi * int64(1+rand.Intn(int(fuzzpercent))/100)
	}

	// turn the path into a lightning route
	plen := len(path)
	route = make([]RouteHop, plen)

	if plen == 1 {
		// single-hop payment, end here
		channel := g.Edge(g.nodeIndex[fromid], g.nodeIndex[id]).(*Channel)
		route[plen-1] = RouteHop{
			Id:       id,
			Msatoshi: msatoshi, // no fees for the last channel, just the fuzz
			Delay:    channel.Delay,
		}
		return route, nil
	}

	// build the route from the ante-last hop backwards
	for i := plen - 2; i > 0; i-- {
		nexthop := route[i+1]
		cur := path[1]
		prev := path[i-1]
		channel := g.channelMap[prev.(*Node).Id][cur.(*Node).Id]
		route[i] = RouteHop{
			Id:       channel.Destination,
			Msatoshi: nexthop.Msatoshi + int64(channel.Fee(nexthop.Msatoshi)),
			Delay:    nexthop.Delay + channel.Delay,
		}
	}

	// add the first channel
	nexthop := route[1]
	cur := path[0]
	channel := g.Edge(g.nodeIndex[fromid], cur.ID()).(*Channel)
	route[0] = RouteHop{
		Id:       cur.(*Node).Id,
		Msatoshi: nexthop.Msatoshi + int64(channel.Fee(nexthop.Msatoshi)),
		Delay:    channel.Delay,
	}

	return
}
