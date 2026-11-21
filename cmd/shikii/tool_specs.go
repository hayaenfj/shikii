package main

// toolSpecs are the ready-made questions that tools built on shikii ask through shikii decide (ids of the embedded specs), beside
// hookSpecs, wikiSpecs and deliverySpecs. chiten asks quote-supports about each source of a research report whose quote it found in
// the cited file. `shikii init` copies them into the project's decisions/ with the others; the person answers them on the page, and
// the Stop hook draws their lines on the same cycle as the wiki's questions.
var toolSpecs = []string{"quote-supports"}
