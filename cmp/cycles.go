package cmp

// cycles is a struct to detect cycles in struct comparing
// It saves the search stack depth whenever pointer type is traversed.
type cycles struct {
	// xDepth and yDepth maps stack depth (value) to a pointer address (key)
	xDepth, yDepth map[uintptr]int
}

// init initiate the data structures of this type
func (c *cycles) init() {
	c.xDepth = make(map[uintptr]int)
	c.yDepth = make(map[uintptr]int)
}

// compare compares cycles that occurred by given pointed addresses
// If an address appears in this struct maps, it means that it was
// already visited in the current comparison path.
// It returns:
//      equal == true if a two detected cycles are equal.
//      ok == true if any cycle was detected.
func (c cycles) compare(xAddr, yAddr uintptr) (equal, ok bool) {
	xDepth, xOk := c.xDepth[xAddr]
	yDepth, yOk := c.yDepth[yAddr]
	return xDepth == yDepth, xOk || yOk
}

// push adds visited addresses to the cycle detector.
// It saves the search stack length so it can be later compared.
// It returns a pop function that removes the pushed addresses, and it
// should be invoked when the search stack is traversed backwards.
func (c *cycles) push(xAddr, yAddr uintptr) (pop func()) {
	// depth is the current cycle depth
	depth := len(c.xDepth) + 1
	c.xDepth[xAddr] = depth
	c.yDepth[yAddr] = depth
	return func() {
		delete(c.xDepth, xAddr)
		delete(c.yDepth, yAddr)
	}
}
