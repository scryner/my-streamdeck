package widgets

func newStateHash() uint64 {
	return 1469598103934665603
}

func addStateHashUint64(sum uint64, value uint64) uint64 {
	sum ^= value
	return sum * 1099511628211
}

func addStateHashInt(sum uint64, value int) uint64 {
	return addStateHashUint64(sum, uint64(int64(value)))
}

func addStateHashInt64(sum uint64, value int64) uint64 {
	return addStateHashUint64(sum, uint64(value))
}

func addStateHashBool(sum uint64, value bool) uint64 {
	if value {
		return addStateHashUint64(sum, 1)
	}
	return addStateHashUint64(sum, 0)
}

func addStateHashString(sum uint64, value string) uint64 {
	for i := 0; i < len(value); i++ {
		sum = addStateHashUint64(sum, uint64(value[i]))
	}
	return sum
}
