package internal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccumulateEdges(t *testing.T) {
	buf := &edgebuf{}
	assert.Equal(t, 0, buf.len())

	producer := make(chan edge)
	consumer := accumulate(producer, buf)

	producer <- edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(1))}
	producer <- edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(2), int64(3))}
	producer <- edge{kind: workEdge, parentID: 100, childIDs: newSet(int64(4))}
	// We unblock as soon as a value is sent down the producer channel but
	// before the buffer is updated. Sleep to allow the other goroutine to allow it
	// to actually push the value into the buffer. Racy but it works for now.
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 4, buf.len())

	e := <-consumer
	assert.Equal(t, edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(1), int64(2), int64(3))}, e)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, buf.len())

	e = <-consumer
	assert.Equal(t, edge{kind: workEdge, parentID: 100, childIDs: newSet(int64(4))}, e)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, buf.len())

	producer <- edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(5), int64(6))}
	producer <- edge{kind: authorEdge, parentID: 200, childIDs: newSet(int64(7))}
	producer <- edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(8))}
	producer <- edge{kind: authorEdge, parentID: 300, childIDs: newSet(int64(9))}
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 5, buf.len())

	e = <-consumer
	assert.Equal(t, edge{kind: authorEdge, parentID: 100, childIDs: newSet(int64(5), int64(6), int64(8))}, e)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 2, buf.len())

	e = <-consumer
	assert.Equal(t, edge{kind: authorEdge, parentID: 200, childIDs: newSet(int64(7))}, e)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, buf.len())

	e = <-consumer
	assert.Equal(t, edge{kind: authorEdge, parentID: 300, childIDs: newSet(int64(9))}, e)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, buf.len())

	close(producer)

	_, ok := <-consumer
	assert.False(t, ok)
}

func TestAccumulateSlice(t *testing.T) {
	buf := slicebuffer[int]{}
	producer := make(chan int)
	consumer := accumulate(producer, &buf)

	// Test this case where we consume before producing.
	go func() {
		time.Sleep(time.Second)
		producer <- -1
	}()
	x := <-consumer
	assert.Equal(t, -1, x)

	producer <- 1
	producer <- 2
	producer <- 3

	n := <-consumer
	assert.Equal(t, 1, n)
	n = <-consumer
	assert.Equal(t, 2, n)
	n = <-consumer
	assert.Equal(t, 3, n)

	close(producer)

	_, ok := <-consumer
	assert.False(t, ok)
}

// A fast consumer used to read the buffer length before accumulate had removed
// the edge it was just handed, which left the gauge at 1 with nothing to do.
func TestDenormWaitingSettlesToZero(t *testing.T) {
	for range 500 {
		metrics := newControllerMetrics(nil)
		producer := make(chan edge)
		consumer := accumulate(producer, &edgebuf{}, metrics.denormBufferedSet)

		processed := make(chan struct{})
		go func() {
			for range consumer {
				metrics.denormInflightAdd(1)
				metrics.denormInflightAdd(-1) // Finishing at once is the case that used to leave the gauge behind.
				processed <- struct{}{}
			}
		}()

		producer <- edge{kind: authorEdge, parentID: 1, childIDs: newSet(int64(10))}
		<-processed

		require.Eventually(t, func() bool { return metrics.denormWaitingGet() == 0 }, 2*time.Second, time.Millisecond)
		close(producer)
	}
}
