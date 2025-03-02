# (Partitioned) Consumer Groups for JetStream

|Metadata| Value               |
|--------|---------------------|
|Date    | 2024-01-31          |
|Author  | @jnmoyne            |
|Status  | `Proposed`          |
|Tags    | jetstream, client   |

| Revision | Date       | Author   | Info                            |
 |---------|------------|----------|---------------------------------|
| 1       | 2024-01-31 | @jnmoyne | Initial design                  |
| 2       | 2025-02-28 | @jnmoyne | Updated to use 2.11 features |

## Problem Statement

Currently distributed message consumption from a stream is possible through the use of durable consumers, however the messages are distributed purely on-demand between the client application processes currently subscribing to the consumer. While this is fine for most message consumption use cases, there are some classes of use cases that require strictly ordered (i.e. one at a time) 'per-key' message consumption.

This means that you must set number of "max pending acknowledgements" to exactly 1 in the consumer, which seriously limits throughput as only one message can be consumed at a time, regardless of the number of client applications subscribing from the consumer. In essence what you want for those use cases is a `max_pending_acks_per_subject` consumer setting, but as that's not available, using partitioning is the way to scale max ack pending=1 consumption.

Besides strict per key ordering in other use cases it is desirable to have a 'per-key stickiness' to the distribution of the messages between the subscribing client applications (e.g. to leverage local caching of high latency key related data better), which is something you can only get through consistent hashing based partitioning.

As side requirement of partitioning is that in order to have HA you need the ability to have only one of those subscribing applications for a partition or group of partitions being selected to be the exclusive recipient of the messages (at a time), with the other ones being in hot standby.

## Context

Kafka's 'consumer groups' provide this exact feature through partitioning, and it is an oft-requested by the NATS community (and by many wanting to migrate from Kafka to NATS JetStream).

## Design goals

The design aims for the following attributes
- The ability to distribute the message consumption of the messages in a stream, while providing ***strictly ordered 'per-key' delivery*** of the messages, when the key used for the consistent hashing distribution of the messages can be any combination of the subject's tokens.
- Fault-tolerance through the deployment of 'standby' consuming client applications that automatically take over the consumption of messages even in the case of process or host failure.
- ***Administratively managed*** 'elasticity': the ability to easily scale up or down the consumption of the messages by increasing or decreasing the number of running consuming client applications.
- ***Always*** keep the strict order of delivery per subject even when scaling up or down.
- Minimization of the re-distribution of keys as the consumer group is scaled up or down.
- Support for both automatic or administratively defined mapping of members to partitions.

## Design

A number of new features of nats-server 2.10 and 2.11 are now making consumer groups possible. Namely: subject transformation in streams with the ability to insert a partition number in a subject token calculated from other subject tokens using a consistent hashing algorithm, multi-filter JS consumers, create (as opposed to create or update) consumer creation call, and pinned consumers.

Initially there are two types of stream consumer groups implemented:

1. Static consumer groups operate on a stream where the partition number has already been inserted in the subject as the first token of the messages. This is not elastic: you create the consumer with a list of members once, and you can not adjust that membership list or mapping for the life of the consumer group (if you want to change the mapping, up to you to delete and re-create the static partitioned consumer group, and to figure out which sequence number you may want this new static partitioned consumer group to start at). You must make sure to specify the stream's number of partitions as the value for the consumer group's max number of members. The consumer group basically takes care of creating and joining the member-specific consumers automatically, the developer only need to provide a stream, consumer group name, a member name and callback. 
2. Elastic consumer groups operate on any stream, the messages in the stream do not have the partition number present in their subjects. The membership list (or mapping) for the consumer can be adjusted administratively at any time and up to the max number of members defined initially. The consumer group in this case creates a new work-queue stream that sources from the stream, inserting the partition number subject token on the way. The consumer group takes care of creating this sourced stream and managing all the consumers on this stream according to the current membership, the developer only needs to provide a stream name, consumer group name and a member name and callback and make sure to ack the messages.

In both cases, for HA you need to launch more than one instance of each member of the consumer group, only one of them will be receiving messages at a time with the other ones in hot standby. This is done using the NATS 2.11 'pinned consumers' functionality.

Elastic partitioned consumer groups are more flexible that static ones, but static partitioned consumer groups are faster and use less resources than elastic ones.

Both kinds support consumer group membership being either a list of member names, in which case the partitions are automatically distributed between the members, or as a set of member mappings which allows the user to completely control the distribution of the partitions over the membership. A member mapping contains a list of member names and for each name the list of partitions mapped to it. To be a valid mapping, all the partition numbers must be assigned to one (and one only) member name.

## Terminology

A ***partitioned consumer group*** is a named new kind of consumer on a stream that distributes messages between ***members*** which are named client application instances wanting to consume (copies of) messages in a stream in a partitioned way.

### Exposed functionalities

For the client application programmer, there is only one basic functionality exposed by both static and elastic partitioned consumer groups: consume messages (when selected) from a named consumer group on a stream by specifying a _member name_ and a _callback_. Although the application programmer must alter the code to go from static to elastic consumer group types, there should only be the need to alter the 'consume' call, everything else (including the callback) doesn't have to change. For elastic partitioned consumer groups the callback should explicitly acknowledge each message, and while static partitioned consumer groups do not need to be explicit acks, if you want max acks pending=1 you will need to have explicit acks.

The rest of the calls are for administrative purposes:

1. For both elastic and static partitioned consumer groups:
- Get a named partitioned consumer group's configuration.
- Create a named partitioned consumer group on a stream by providing a configuration.
- Delete a named partitioned consumer group on a stream.
- List the partitioned consumer groups defined on a stream.
- List the partitioned consumer group's members that are currently consuming from the partitioned consumer group.
- For the currently active instance of a member to step down.

2. Elastic partitioned consumer groups adds the following:

- Add members to a named partitioned consumer group.
- Drop members from a partitioned consumer group.
- Set, update or delete a custom member-to-partitions mapping.
- When creating an elastic partitioned consumer group, you must provide a filter, with at least one `*` wildcard, and then indicate the wildcard index number(s) that you want to use to consistent hash upon.

### Partitioned consumer group configuration

The design relies on using KV buckets to store the configuration information for the consumer groups. For static, the KV bucket name is `static-consumer-groups` while for elastic it is `elastic-consumer-groups` . The key is the combination of the stream name and the partitioned consumer group's name. You can create any number of named partitioned consumer groups on a stream, just like you can create any number of consumers on a stream.

#### Static Configuration
- The stream name
- The maximum number of members
- An optional subject filter (to further filter the messages from the stream, the filter is for the part of the subject beyond the first token (which contains the partition number and is removed automatically))
- A list of member names _OR_ a set of member mappings

As the name implies, once the static consumer group is created, it should ***not*** be updated, and always deleted then re-created instead.

This is the format of the config records stored in the `static-consumer-groups` KV bucket.
```go
type StaticConsumerGroupConfig struct {
MaxMembers     uint            `json:"max_members"`
Filter         string          `json:"filter"`
Members        []string        `json:"members,omitempty"`
MemberMappings []MemberMapping `json:"member-mappings,omitempty"`
}
```

#### Elastic
- The stream name
- The maximum number of members
- A subject filter, that must contain at least one `*` wildcard.
- A list of the subject filter partitioning wildcard indexes that will be used to make the 'key' used for the distribution of messages. For example if the subject filter is `"foo.*.*.>"` (two wildcard indexes: 1 and 2) the list would be `[1,2]` if the key is composed of both `*` wildcards, `[1]` if the key is composed of only the first `*` wildcard of the filter, or `[2]` if is it composed of only the second `*` wildcard.
- The current list of partitioned consumer group member names _OR_ the current list of consumer group members to partition mappings.

Note: once an elastic partitioned consumer group has been created, the list of members or the list member mappings are the ***only*** part of its configuration that can be changed.

This is the format of the config records stored in the `elastic-consumer-groups` KV bucket.
```go
type ElasticConsumerGroupConfig struct {
	MaxMembers            uint            `json:"max_members"`
	Filter                string          `json:"filter"`
	PartitioningWildcards []int           `json:"partitioning-wildcards"`
	MaxBufferedMsgs       int64           `json:"msg-buffer-size,omitempty"`
	Members               []string        `json:"members,omitempty"`
	MemberMappings        []MemberMapping `json:"member-mappings,omitempty"`
}
```

#### Mermber mapping format

This is the format of a Member mapping entry:
```go
type MemberMapping struct {
	Member     string `json:"member"`
	Partitions []int  `json:"partitions"`
}
```
### Creating and deleting consumer groups
Creating a static consumer group means only putting the config record in the KV bucket. That config record is then read by the member client application instances who will automatically create their own consumers on the stream as needed.

Creating an elastic consumer group means not only putting the config record in the KV bucket but also creating the underlying sourcing work queue stream.

Conversely, deleting a consumer group means removing the config entry for it in the KV bucket. Plus in the case of elastic consumer groups, deleting the underlying sourcing stream. And in the case of static consumer groups,  deleting the consumers for the group.
### Partitioned consumer group members (consuming client applications)

Client applications that want to consume from the partitioned consumer group typically would either create the consumer group themselves (or rely on it being created administratively ahead of time) just like they would use a regular durable consumer. And would then signal their ability to consume messages by calling `StaticConsume` or `ElasticConsume` (it could also be described as 'joining') with a specific member name. Joining doesn't mean that messages will actually start being delivered to the client application, as that is controlled by two factors: the member's name being listed in the current list or members for the partitioned consumer group's config, and, if more than one instance of the same member are joined to the consumer group at the time, being selected as the 'pinned' client application instance. The member client instance may start or stop receiving messages at any moment.

Both consume calls take the exact same set of arguments:
 - (in Go) A `context.Context` (e.g. if you want to cancel it)
 - A NATS connection pointer to use
 - The stream name
 - The consumer group name
 - A callback to process the message (same kind you would use for subscribing to a regular consumer, the partition number in the subject token of the message is automatically dropped before invoking the callback)
 - A regular `jetstream.ConsumerConfig` struct. This allows the programmer to specify most of the consumer attributes (see below)

The `ConsumerConfig` struct passed by the programmer has a few of its fields overwritten and is then used to create the actual consumers on the stream. The consumer's name, durable and filters are always overwritten, AckWait if not specified will be set to 6 seconds, and the priority groups, policies, and pinned TTL will also be overwritten. In addition, for elastic consumer groups, the Ack Policy is set to explicit, and the inactiveThreshold if not specified is set to 12 seconds.

### Elasticity

People too often will associate elasticity of a partitioning system with having to change the number of partitions as the membership in the group changes over time, and re-partitioning is always going to be a difficult and expensive thing to do, and therefore not something you want to do all the time. In practice, however, there is a way to achieve acceptable elasticity without providing 'infinite elasticity'.

The trick is simply to see the number of partitions as the maximum number of members, and then you distribute those partitions as evenly as you can between the members. Partitions in NATS' case are cheap, they are literally nothing more than 'just another subject (filter on a consumer)' so you don't have to try and be too stingy with the number of them. Then you just need to have a coordinated way to move partitions between members, and you have elasticity.

These things are implemented on top of JetStream using new 2.10 and 2.11 features. The mapping of members to partitions is done by having each (set of) member(s) create a named consumer for the member (with a relatively short idle timeout), and expressing the partitions that this particular member was assigned to as filter subjects in the consumer's configuration. The coordination trick required to be able to be elastic is done by doing this on top of a working queue stream, which ensures that no two member consumers can have the same partition in their subject filters at the same time and means you can delete and re-create the member's consumer each time the membership changes without having to worry about where each member was in terms of it's position in the stream (messages are deleted from the stream as they are consumed).

So when you create an elastic partitioned consumer group, you actually create a new working queue stream, that sources from the initial stream and inserts the partition number token in the subject at the same time, and on which the members create their consumers. The obvious down-side is that this can lead to requiring the space to store the data twice in two streams (unless you limit the size of that work queue stream to only have it store a portion of the messages in the stream being sourced), but the advantages are that guarantee that a working-queue stream will never allow two members to operate on the same partition at the same time, and it also can give you a convenient way to track the progress of the members in real-time (the size of that working-queue, up to the limits mentioned before), which is something you could use to try to automate the scaling up or down, and finally using a working queue stream allows the creation/deletion of the consumers on that stream without having to worry about having to store and specify a starting sequence number in order to not re-consume messages.

#### Transitioning as the membership changes

When membership changes in an elastic consumer group the partitions can be re-distributed and moved from one member to another (and accordingly the members consumers' subject filters have to change). This kind of coordinated transition is very difficult to do seamlessly. Also, you can not have a single partition being consumed by more than one member at any time during the transition. This latter constraint is enforced by the use of a work queue stream as it does not allow any overlap of filters between two consumers. Therefore, the transition is done in two steps: as soon as consuming client applications get the config update from the KV bucket, the currently pinned instance first deletes the current member's consumer, and then try to re-create the consumer again with the new filters. In order to avoid flapping of the active member instance on membership changes the currently active instance waits does not wait before trying to create the new version of the consumer while the others wait 250ms before trying to create the consumer themselves. The consumers are created with an inactivity timeout of just 12 seconds, but could be smaller. Also all client instances are always going to retry creating and using the consumer every 15 seconds if the creation fails (as the config must match exactly).

##### At least once vs exactly once and acking considerations

For elastic consumer groups: during a change in membership (and during some failure scenarios where the currently pinned consumer instance gets suspended or isolated for some period of time) there is a possibility that a message gets re-delivered (to another instance of the member) by the time the message gets acknowledged in the user's callback (or that the consumer is deleted by that time), in all cases the message is not lost and gets re-delivered, however at this point it impossible for the client code to tell that the message has already been processed and acknowledged by the time the code tries to acknowledge it, even if synchronous 'double acks' are used because currently NATS does not return any status in the acknowledgement replies and therefore there is no indication that the message was already acked before.

This means that (until improvements are made to acknowledgement in consumers) the only quality of service we can claim is 'at least once' (and not 'exactly once consumption' (meaning the message may get delivered more than once, but you have a way to ensure that it gets processed only exactly once (i.e. it has already been ack'd))).

#### Details

1. Partition to member distribution:
The partition to member distribution algorithm is designed to minimize the number of partitions that get re-assigned from one member to another as the member list grows/shrinks: rather than being a simple modulo (which would suffice for distribution purposes) the algorithm is the following:

- De-duplicate the list of members in the consumer group's configuration
- Sort the list
- Cap the list to the max number of members
- (Unless a custom specific set of members to partition mappings is defined) Distribute automatically the partitions amongst the members in the list, assigning continuous blocks of partitions of size (number of partitions divided by the number of members in the list), and finally distribute the remainder (if the number of members in the list is not a multiple of the number of partitions) over the members using a modulo.

Automatic partition to member mapping implementation:

```go
func GeneratePartitionFilters(members []string, maxMembers uint, memberMappings []MemberMapping, memberName string) []string {
	if len(members) != 0 {
		members := deduplicateStringSlice(members)
		slices.Sort(members)

		if uint(len(members)) > maxMembers {
			members = members[:maxMembers]
		}

		// Distribute the partitions amongst the members trying to minimize the number of partitions getting re-distributed
		// to another member as the number of members increases/decreases
		numMembers := uint(len(members))

		if numMembers > 0 {
			// rounded number of partitions per member
			var numPer = maxMembers / numMembers
			var myFilters []string

			for i := uint(0); i < maxMembers; i++ {
				var memberIndex = i / numPer

				if i < (numMembers * numPer) {
					if members[memberIndex%numMembers] == memberName {
						myFilters = append(myFilters, fmt.Sprintf("%d.>", i))
					}
				} else {
					// remainder if the number of partitions is not a multiple of the number of members
					if members[(i-(numMembers*numPer))%numMembers] == memberName {
						myFilters = append(myFilters, fmt.Sprintf("%d.>", i))
					}
				}
			}

			return myFilters
		}
		return []string{}
	} else if len(memberMappings) != 0 {
		var myFilters []string

		for _, mapping := range memberMappings {
			if mapping.Member == memberName {
				for _, pn := range mapping.Partitions {
					myFilters = append(myFilters, fmt.Sprintf("%d.>", pn))
				}
			}
		}

		return myFilters
	}
	return []string{}
}
```
2. Removing the partition token from the message subject
The partition number token is stripped from the subject before being passed down to the user's message consumption callback.

3. User callback code
It is up to the client application consumption callback code to acknowledge the message. For elastic partitioned consumer groups, explicit acking _MUST_ be used. In order to achieve exactly once processing the client application callback code should follow this pattern: process the message begin a transaction on the destination(s) systems of record that get modified by the processing of the message, call prepare on all of them and if they all are prepared call `DoubleAck()` on the message, if that returns without an error then commit the transaction(s) but if the double ack does return an error (because for example the process was suspended or isolated for a period of time longer than what it takes for another member instance to be selected to consume the messages, in which case the newly active member instance will get all currently un-acknowledged messages from the consumer group's stream, or because the membership was changed while the client application was still in the middle of processing the message and not having acknowledged it by the time the membership change is processed) then it should roll back the transaction(s) to avoid double-processing.

## Administration

By simply using the consumer group config KV bucket, administrators can:
- Create consumer groups on streams -> creates a new config entry
- Delete consumer groups on streams -> deletes the config entry
- List consumer groups on a stream -> list the keys matching "<stream name>.*"
- Add/Remove members to a consumer group -> add/remove a member name to the `members` array in the config entry, or Create/Delete a set of custom member names to partition number mappings -> create/delete the `member-mappings` array in the config entry
- Request the current active member instance for a member to step down and have another member instance currently joined (if there's one take over as the current active member instance)

You can also see the list of members that have currently active instances by looking at what consumer currently exist on the consumer group's stream, which is important because you therefore know when you are missing any and which ones are missing meaning that some of the partitions are not being consumed.

## Reference initial implementation
https://github.com/synadia-labs/partitioned-consumer-groups